package db

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/dto"
	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

var _ appservice.InsightsQueryStore = (*InsightsQueryRepository)(nil)

// InsightsQueryRepository 实现健康面板的四类只读聚合（spec §7）。
// 行类型定义在 application/service（分层惯例：db 依赖 application）。
// 全部 SQL 双方言兼容（不使用 DISTINCT ON 等 PG 专有语法）。
type InsightsQueryRepository struct {
	db *gorm.DB
}

// NewInsightsQueryRepository creates an insights query repository.
func NewInsightsQueryRepository(db *gorm.DB) *InsightsQueryRepository {
	return &InsightsQueryRepository{db: db}
}

// CountRunsByStatus 统计窗口内参与该 KB 的 run 状态分布。
// 口径（spec §4.5）：排除 transport='console' 的管理台调试流量；
// running 不计入（未完成）；failed 计入返回值但由 service 层剔除分母。
func (r *InsightsQueryRepository) CountRunsByStatus(
	ctx context.Context, workspaceID, kbID uuid.UUID, from, to time.Time,
) ([]appservice.InsightsRunStatusCount, error) {
	var rows []appservice.InsightsRunStatusCount
	err := r.db.WithContext(ctx).Raw(`
		SELECT sr.retrieval_status AS retrieval_status, COUNT(DISTINCT sr.id) AS runs
		FROM search_runs sr
		JOIN search_run_generations srg
		  ON srg.workspace_id = sr.workspace_id AND srg.search_run_id = sr.id
		  AND srg.knowledge_base_id = ?
		WHERE sr.workspace_id = ?
		  AND sr.transport <> 'console'
		  AND sr.retrieval_status <> 'running'
		  AND sr.created_at >= ? AND sr.created_at < ?
		GROUP BY sr.retrieval_status
	`, kbID, workspaceID, from, to).Scan(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "统计检索运行状态失败")
	}
	return rows, nil
}

// ZeroRecallGroups 聚合窗口内零召回查询（按 query_hash；transport=console 排除）。
func (r *InsightsQueryRepository) ZeroRecallGroups(
	ctx context.Context, workspaceID, kbID uuid.UUID, from, to time.Time, limit int,
) ([]appservice.InsightsZeroRecallGroup, error) {
	var rows []appservice.InsightsZeroRecallGroup
	err := r.db.WithContext(ctx).Raw(`
		SELECT sr.query_hash AS query_hash, MAX(sr.query_chars) AS query_chars,
		       COUNT(DISTINCT sr.id) AS hits,
		       MIN(sr.created_at) AS first_seen, MAX(sr.created_at) AS last_seen
		FROM search_runs sr
		JOIN search_run_generations srg
		  ON srg.workspace_id = sr.workspace_id AND srg.search_run_id = sr.id
		  AND srg.knowledge_base_id = ?
		WHERE sr.workspace_id = ?
		  AND sr.retrieval_status = 'empty'
		  AND sr.transport <> 'console'
		  AND sr.created_at >= ? AND sr.created_at < ?
		GROUP BY sr.query_hash
		ORDER BY hits DESC, last_seen DESC
		LIMIT ?
	`, kbID, workspaceID, from, to, limit).Scan(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "聚合零召回查询失败")
	}
	return rows, nil
}

// LatestQueryTexts 读取窗口内各 query_hash 的最新原文（query_log）。
// 同一 hash 并列最新时间戳的多行由调用侧 map 去重（保留任一）。
func (r *InsightsQueryRepository) LatestQueryTexts(
	ctx context.Context, workspaceID, kbID uuid.UUID, from time.Time,
) ([]appservice.InsightsLatestQueryText, error) {
	var rows []appservice.InsightsLatestQueryText
	err := r.db.WithContext(ctx).Raw(`
		SELECT ql.query_hash AS query_hash, ql.query_text AS query_text
		FROM search_query_log ql
		JOIN (
		    SELECT query_hash, MAX(created_at) AS latest
		    FROM search_query_log
		    WHERE workspace_id = ? AND kb_id = ? AND created_at >= ?
		    GROUP BY query_hash
		) latest ON latest.query_hash = ql.query_hash AND latest.latest = ql.created_at
		WHERE ql.workspace_id = ? AND ql.kb_id = ?
	`, workspaceID, kbID, from, workspaceID, kbID).Scan(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "读取查询原文失败")
	}
	return rows, nil
}

// SleepingDocuments 返回窗口起点前已就绪、窗口内零引用、未删除的文档（anti-join）。
// indexed_at 用 documents.updated_at 近似（无独立索引时间列）；chunk_count 省略
// （避免跨 Generation 聚合，取舍记录于实现计划）。
func (r *InsightsQueryRepository) SleepingDocuments(
	ctx context.Context, workspaceID, kbID uuid.UUID, windowStart time.Time, limit int,
) ([]dto.InsightsSleepingDocument, error) {
	var rows []dto.InsightsSleepingDocument
	err := r.db.WithContext(ctx).Raw(`
		SELECT d.id AS document_id, d.kind AS kind, d.title AS title,
		       d.updated_at AS indexed_at
		FROM documents d
		WHERE d.workspace_id = ? AND d.knowledge_base_id = ?
		  AND d.deleted_at IS NULL AND d.status = 'ready'
		  AND d.updated_at < ?
		  AND NOT EXISTS (
		      SELECT 1 FROM search_citations sc
		      WHERE sc.workspace_id = d.workspace_id
		        AND sc.kb_id = d.knowledge_base_id
		        AND sc.document_id = d.id
		        AND sc.created_at >= ?
		  )
		ORDER BY d.updated_at ASC
		LIMIT ?
	`, workspaceID, kbID, windowStart, windowStart, limit).Scan(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "查询沉睡内容失败")
	}
	return rows, nil
}

// RecentActivity 返回近窗内容动态（三类事件各查一段、Go 侧归并排序截断）。
// limit 是最终返回条数；每段查询取 limit 条保证覆盖。
func (r *InsightsQueryRepository) RecentActivity(
	ctx context.Context, workspaceID, kbID uuid.UUID, since time.Time, limit int,
) ([]dto.InsightsActivityEvent, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: activity limit 必须为正", domainerrors.ErrValidation)
	}
	events := make([]dto.InsightsActivityEvent, 0, limit*2)

	// 1) 源同步完成/失败（web 源级 + 飞书 KB 级 source_sync job 终态）。
	var syncRows []dto.InsightsActivityEvent
	if err := r.db.WithContext(ctx).Raw(`
		SELECT 'sync' AS kind, j.kb_source_id AS source_id,
		       COALESCE(src.name, kb.name, '') AS source_name,
		       CASE WHEN j.status = 'completed' THEN 1 ELSE 0 END AS success,
		       j.error_message AS error_message, j.updated_at AS occurred_at
		FROM jobs j
		LEFT JOIN kb_sources src
		  ON src.workspace_id = j.workspace_id AND src.id = j.kb_source_id
		LEFT JOIN knowledge_bases kb
		  ON kb.workspace_id = j.workspace_id AND kb.id = j.knowledge_base_id
		WHERE j.workspace_id = ? AND j.knowledge_base_id = ?
		  AND j.type = 'source_sync' AND j.status IN ('completed', 'failed')
		  AND j.updated_at >= ?
		ORDER BY j.updated_at DESC
		LIMIT ?
	`, workspaceID, kbID, since, limit).Scan(&syncRows).Error; err != nil {
		return nil, translateDBError(err, "查询源同步动态失败")
	}
	events = append(events, syncRows...)

	// 2) 内容新版本（document_revisions，含 crawl/edit/replace 等 reason）。
	var revRows []dto.InsightsActivityEvent
	if err := r.db.WithContext(ctx).Raw(`
		SELECT 'revision' AS kind, dr.document_id AS target_document_id,
		       d.kind AS document_kind, d.title AS document_title,
		       dr.revision_reason AS revision_reason, dr.created_at AS occurred_at
		FROM document_revisions dr
		JOIN documents d
		  ON d.workspace_id = dr.workspace_id AND d.knowledge_base_id = dr.knowledge_base_id
		  AND d.id = dr.document_id
		WHERE dr.workspace_id = ? AND dr.knowledge_base_id = ? AND dr.created_at >= ?
		ORDER BY dr.created_at DESC
		LIMIT ?
	`, workspaceID, kbID, since, limit).Scan(&revRows).Error; err != nil {
		return nil, translateDBError(err, "查询内容版本动态失败")
	}
	events = append(events, revRows...)

	// 3) 内容软删（deleted_at 近窗）。
	var delRows []dto.InsightsActivityEvent
	if err := r.db.WithContext(ctx).Raw(`
		SELECT 'delete' AS kind, d.id AS target_document_id,
		       d.kind AS document_kind, d.title AS document_title,
		       d.deleted_at AS occurred_at
		FROM documents d
		WHERE d.workspace_id = ? AND d.knowledge_base_id = ?
		  AND d.deleted_at IS NOT NULL AND d.deleted_at >= ?
		ORDER BY d.deleted_at DESC
		LIMIT ?
	`, workspaceID, kbID, since, limit).Scan(&delRows).Error; err != nil {
		return nil, translateDBError(err, "查询内容删除动态失败")
	}
	events = append(events, delRows...)

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].OccurredAt.After(events[j].OccurredAt)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

// DocumentCitationSummary 返回文档在窗口内的引用次数与最近引用时间（删除预警）。
// 文档已删或跨 workspace 由调用侧 KB/Document 归属校验兜底；本查询只按行存在性统计
// （D3：文档删除后引用行仍在，预警依赖该事实）。
func (r *InsightsQueryRepository) DocumentCitationSummary(
	ctx context.Context, workspaceID, kbID, documentID uuid.UUID, since time.Time,
) (count int64, lastCitedAt *time.Time, err error) {
	var row struct {
		Count       int64
		LastCitedAt *time.Time
	}
	err = r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*) AS count, MAX(created_at) AS last_cited_at
		FROM search_citations
		WHERE workspace_id = ? AND kb_id = ? AND document_id = ? AND created_at >= ?
	`, workspaceID, kbID, documentID, since).Scan(&row).Error
	if err != nil {
		return 0, nil, translateDBError(err, "统计文档引用摘要失败")
	}
	return row.Count, row.LastCitedAt, nil
}
