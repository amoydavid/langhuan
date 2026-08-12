package db

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// RetrievalRepository persists the rebuildable FTS/vector projection.
//
// 持有方言与可选分词器：PG 在 SQL 层用 to_tsvector/plainto_tsquery + zhparser，
// tokenizer 为 nil；SQLite 把 embedding/fts 拆到独立投影表，且 FTS5 无法注册自定义
// tokenizer（modernc.org/sqlite 不暴露该能力），中文分词必须在应用层完成，
// 因此 SQLite 装配必须注入非 nil 的 SearchTokenizer（spec §7.2、§8.2）。
type RetrievalRepository struct {
	db        *gorm.DB
	dialect   Dialect
	tokenizer SearchTokenizer
}

// NewRetrievalRepository creates a RetrievalEntry repository.
//
// tokenizer 仅 SQLite 路径消费（写 FTS5 tokenized 列 + 构造 FTS5 MATCH 表达式）；
// PG 装配传 nil（PG 在 SQL 层 to_tsvector/plainto_tsquery）。SQLite 装配由切片7
// 注入 gse adapter；当前 main.go 仍传 nil（仅 PG 运行，零回归）。
func NewRetrievalRepository(database *gorm.DB, tokenizer SearchTokenizer) *RetrievalRepository {
	dialect, err := DialectOf(database)
	if err != nil {
		// db.Open 已保证 Dialector 非空且为 postgres/sqlite，此处不可达；
		// 兜底按 postgres 处理，保证构造不因未知方言失败。
		dialect = DialectPostgres
	}
	return &RetrievalRepository{db: database, dialect: dialect, tokenizer: tokenizer}
}

// StageBatch atomically replaces staging entries for the same Generation/Chunks.
func (r *RetrievalRepository) StageBatch(
	ctx context.Context,
	workspaceID uuid.UUID,
	ftsConfig string,
	dimension int,
	entries []indexport.StageEntry,
) error {
	if workspaceID == uuid.Nil || strings.TrimSpace(ftsConfig) == "" || !value.IsSupportedEmbeddingDimension(dimension) {
		return fmt.Errorf("%w: Retrieval staging Workspace/FTS/dimension 无效", domainerrors.ErrValidation)
	}
	rows := make([]*RetrievalEntryRow, len(entries))
	for index, staged := range entries {
		if err := validateStagingEntry(workspaceID, dimension, staged); err != nil {
			return fmt.Errorf("Retrieval staging entry %d: %w", index, err)
		}
		row, err := retrievalEntryToRow(staged.Entry)
		if err != nil {
			return err
		}
		// retrieval_entries 在两种方言下都不在 INSERT 阶段写投影列：
		// PG 由随后的 UPDATE 回填 embedding::halfvec / fts_document=to_tsvector；
		// SQLite 这两列不存在（独立投影表），由 Omit 跳过。dimension 由各自分支回填。
		row.FTSDocument = ""
		row.Embedding = nil
		row.Dimension = nil
		rows[index] = row
	}
	if len(rows) == 0 {
		return nil
	}
	if r.dialect == DialectSQLite {
		return r.stageBatchSQLite(ctx, workspaceID, dimension, entries, rows)
	}
	return r.stageBatchPostgres(ctx, workspaceID, ftsConfig, dimension, entries, rows)
}

// stageBatchPostgres 保留原 PG 逻辑：CreateInBatches 后用 UPDATE 回填
// embedding::halfvec 与 fts_document=to_tsvector（spec §7.1、§8.1）。
func (r *RetrievalRepository) stageBatchPostgres(
	ctx context.Context,
	workspaceID uuid.UUID,
	ftsConfig string,
	dimension int,
	entries []indexport.StageEntry,
	rows []*RetrievalEntryRow,
) error {
	vectors := make([]string, len(entries))
	for index, staged := range entries {
		vectors[index] = halfVectorLiteral(staged.Embedding)
	}
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		for _, row := range rows {
			if err := tx.WithContext(ctx).
				Where(
					"workspace_id = ? AND index_generation_id = ? AND chunk_id = ? AND state = ?",
					workspaceID, row.IndexGenerationID, row.ChunkID, value.RetrievalEntryStaging,
				).Delete(&RetrievalEntryRow{}).Error; err != nil {
				return translateDBError(err, "清理旧 RetrievalEntry staging 失败")
			}
		}
		if err := tx.WithContext(ctx).CreateInBatches(rows, 200).Error; err != nil {
			return translateDBError(err, "批量创建 RetrievalEntry staging 失败")
		}
		for index, row := range rows {
			result := tx.WithContext(ctx).Exec(
				"UPDATE retrieval_entries "+
					"SET fts_document = to_tsvector(?::regconfig, search_content), "+
					"embedding = ?::halfvec, dimension = ? "+
					"WHERE workspace_id = ? AND id = ? AND state = ?",
				ftsConfig, vectors[index], dimension, workspaceID, row.ID, value.RetrievalEntryStaging,
			)
			if result.Error != nil {
				return translateFTSConfigError(
					result.Error, ftsConfig, "写入 RetrievalEntry FTS/Embedding 失败",
				)
			}
			if result.RowsAffected != 1 {
				return domainerrors.ErrNotFound
			}
		}
		return nil
	})
}

// stageBatchSQLite 把向量与全文检索写入独立投影表（spec §7.2、§8.2）：
//   - retrieval_entries 不含 embedding/fts_document 列，CreateInBatches 必须 Omit 这两列；
//   - retrieval_embeddings 存 float32 little-endian BLOB，检索用 vec_distance_cosine 精确扫描；
//   - retrieval_fts 存 gse 分词后的 search_content_tokenized，entry_id UNINDEXED 仅用于 JOIN 回查。
//
// 所有写入在同一 Workspace 事务内。tokenizer 是 SQLite 必需依赖（FTS5 无法注册自定义
// tokenizer），nil 视为装配错误。
func (r *RetrievalRepository) stageBatchSQLite(
	ctx context.Context,
	workspaceID uuid.UUID,
	dimension int,
	entries []indexport.StageEntry,
	rows []*RetrievalEntryRow,
) error {
	if r.tokenizer == nil {
		return fmt.Errorf("%w: SQLite retrieval staging 缺少分词器", domainerrors.ErrValidation)
	}
	dim := dimension
	for _, row := range rows {
		row.Dimension = &dim
	}
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		for _, row := range rows {
			// retrieval_embeddings 经 FK ON DELETE CASCADE 随 retrieval_entries 级联删除；
			// retrieval_fts 是 FTS5 虚拟表，无法建 FK，需显式清理其孤儿行，避免重建时残留。
			if err := tx.WithContext(ctx).Exec(
				"DELETE FROM retrieval_fts WHERE entry_id IN ("+
					"SELECT id FROM retrieval_entries "+
					"WHERE workspace_id = ? AND index_generation_id = ? AND chunk_id = ? AND state = ?)",
				workspaceID, row.IndexGenerationID, row.ChunkID, value.RetrievalEntryStaging,
			).Error; err != nil {
				return translateDBError(err, "清理旧 RetrievalEntry FTS 孤儿失败")
			}
			if err := tx.WithContext(ctx).
				Where(
					"workspace_id = ? AND index_generation_id = ? AND chunk_id = ? AND state = ?",
					workspaceID, row.IndexGenerationID, row.ChunkID, value.RetrievalEntryStaging,
				).Delete(&RetrievalEntryRow{}).Error; err != nil {
				return translateDBError(err, "清理旧 RetrievalEntry staging 失败")
			}
		}
		if err := tx.WithContext(ctx).Omit("embedding", "fts_document").
			CreateInBatches(rows, 200).Error; err != nil {
			return translateDBError(err, "批量创建 RetrievalEntry staging 失败")
		}
		for index, row := range rows {
			blob := encodeFloat32LE(entries[index].Embedding)
			if err := tx.WithContext(ctx).Exec(
				"INSERT INTO retrieval_embeddings "+
					"(entry_id, workspace_id, knowledge_base_id, index_generation_id, dimension, embedding) "+
					"VALUES (?, ?, ?, ?, ?, ?)",
				row.ID, workspaceID, row.KnowledgeBaseID, row.IndexGenerationID, dimension, blob,
			).Error; err != nil {
				return translateDBError(err, "写入 RetrievalEntry 向量投影失败")
			}
			tokenized := strings.Join(r.tokenizer.Tokens(row.SearchContent), " ")
			if err := tx.WithContext(ctx).Exec(
				"INSERT INTO retrieval_fts (entry_id, search_content_tokenized) VALUES (?, ?)",
				row.ID, tokenized,
			).Error; err != nil {
				return translateDBError(err, "写入 RetrievalEntry FTS 投影失败")
			}
		}
		return nil
	})
}

func validateStagingEntry(workspaceID uuid.UUID, dimension int, staged indexport.StageEntry) error {
	entry := staged.Entry
	if entry == nil || entry.ID == uuid.Nil || entry.WorkspaceID != workspaceID ||
		entry.KnowledgeBaseID == uuid.Nil || entry.IndexGenerationID == uuid.Nil ||
		entry.DocumentID == uuid.Nil || entry.DocumentRevisionID == uuid.Nil ||
		entry.ChunkSetID == uuid.Nil || entry.ChunkID == uuid.Nil || entry.ChunkRevisionID == uuid.Nil ||
		entry.State != value.RetrievalEntryStaging || strings.TrimSpace(entry.SearchContent) == "" ||
		strings.TrimSpace(entry.Content) == "" || len(staged.Embedding) != dimension {
		return fmt.Errorf("%w: RetrievalEntry lineage/content/vector 无效", domainerrors.ErrValidation)
	}
	for _, component := range staged.Embedding {
		if math.IsNaN(float64(component)) || math.IsInf(float64(component), 0) {
			return domainerrors.ErrInvalidEmbeddingResponse
		}
	}
	return nil
}

// halfVectorLiteral 把 float32 向量序列化为 PG halfvec 字面量 [a,b,c]。
// SQLite 向量检索同样复用它作为 vec_f32(?) 的 JSON 入参（vec_f32 接受 [a,b,c]），
// 因此两种方言共用本函数，无需方言分支。
func halfVectorLiteral(vector []float32) string {
	buffer := make([]byte, 0, len(vector)*8+2)
	buffer = append(buffer, '[')
	for index, component := range vector {
		if index > 0 {
			buffer = append(buffer, ',')
		}
		buffer = strconv.AppendFloat(buffer, float64(component), 'g', -1, 32)
	}
	buffer = append(buffer, ']')
	return string(buffer)
}

// encodeFloat32LE 把 float32 向量序列化为 little-endian BLOB，供 sqlite-vec
// 的 vec_distance_cosine 直接消费（spec §7.2）。维度 CHECK 在迁移层保证。
func encodeFloat32LE(vector []float32) []byte {
	buffer := make([]byte, 4*len(vector))
	for index, component := range vector {
		binary.LittleEndian.PutUint32(
			buffer[index*4:index*4+4],
			math.Float32bits(component),
		)
	}
	return buffer
}
