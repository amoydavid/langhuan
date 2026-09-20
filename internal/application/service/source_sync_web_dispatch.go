package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件承载 web 内容源（kb_sources.kind='web'）的调度侧用例（v1.3.0 spec §9.1）：
// Meta Scheduler / 后续 API 触发入口统一经 EnqueueSyncForSource 入队，与执行侧
// （source_sync_web.go 的 RunSourceSyncJobForSource）解耦。职责：
//   - 源级 source_sync Job 的幂等创建（同源串行由 store 事务内 reuse-or-create 保证）；
//   - 任务超时按源推导（min(页数基准 × 单页耗时 × 2, 硬上限)），显式覆盖队列全局
//     默认超时，防止触及 max_pages_ceiling 的长爬取陷入"超时→重试→再超时"循环。
//
// 执行隔离动机（spec §9.1）：asynq 队列权重只是调度优先级、全局 queue.concurrency
// 由全部任务类型共享，不做在飞封顶的话，多个长爬取会挤占 parse/index 等短任务的
// worker 槽。在飞上限与分组限流由 Meta Scheduler（source_sync_scheduler.go）管辖。

const (
	// defaultWebRequestTimeout 是超时推导的单请求超时兜底（config request_timeout_seconds 默认）。
	defaultWebRequestTimeout = 30 * time.Second
	// defaultWebRequestDelay 是超时推导的请求间隔兜底（config default_request_delay_ms 默认）。
	defaultWebRequestDelay = 200 * time.Millisecond
	// defaultWebSyncTaskTimeout 是单次源同步任务超时硬上限兜底（config sync_task_timeout_seconds 默认 4h）。
	defaultWebSyncTaskTimeout = 4 * time.Hour
	// webSitemapPageBasis 是 sitemap 模式超时推导的页数基准：发现完成前页面数不可知，
	// 取与 web 抓取页缓存上限（internal/adapters/source/web 的 pageCacheMaxCapacity=500）
	// 同源的值；crawl.max_pages 缺省值同为 500，调整两处时需同步。
	webSitemapPageBasis = 500
)

// webSyncPageBasis 从源 config 原始 map 推导超时估算的页数基准（spec §9.1）：
// crawl=crawl.max_pages、urls=len(urls)、sitemap=webSitemapPageBasis。
// config 缺键/类型异常时按对应模式缺省值兜底（与 value.ParseWebSourceConfig 的
// 缺省语义一致），未知 mode 兜底为 sitemap 基准。页数至少为 1，避免空 urls 源
// 推导出必然不够用的超时。
func webSyncPageBasis(config map[string]any) int {
	pages := 0
	switch modeOf(config) {
	case value.WebSourceModeCrawl:
		if n := webConfigInt(config["crawl"], "max_pages"); n > 0 {
			pages = n
		} else {
			pages = webSitemapPageBasis
		}
	case value.WebSourceModeURLs:
		pages = webConfigSliceLen(config["urls"])
	case value.WebSourceModeSitemap:
		pages = webSitemapPageBasis
	default:
		pages = webSitemapPageBasis
	}
	if pages < 1 {
		pages = 1
	}
	return pages
}

// webSyncTaskTimeoutFor 按源推导单次 web 源同步任务的队列超时（spec §9.1）：
// min(页数基准 × (request_timeout + request_delay) × 2, WebSyncTaskTimeout 硬上限)。
// 推导值必须高于队列全局默认（30 分钟），否则大 max_pages 源会被过早终止。
func (s *SourceSyncService) webSyncTaskTimeoutFor(source *model.KbSource) time.Duration {
	config := map[string]any{}
	if source != nil {
		config = source.Config
	}
	perPage := s.webRequestTimeout + s.webDefaultRequestDelay
	derived := time.Duration(webSyncPageBasis(config)) * perPage * 2
	if derived > s.webSyncTaskTimeout {
		return s.webSyncTaskTimeout
	}
	return derived
}

// modeOf 读取源 config 的 mode 字段（缺失/非法返回空，交由 default 分支兜底）。
func modeOf(config map[string]any) value.WebSourceMode {
	raw, _ := config["mode"].(string)
	return value.WebSourceMode(raw)
}

// webConfigInt 从 config 子对象读取整型字段（JSON 解码产物为 float64，兼容 int）。
func webConfigInt(obj any, key string) int {
	m, ok := obj.(map[string]any)
	if !ok {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0
		}
		return int(n)
	default:
		return 0
	}
}

// webConfigSliceLen 返回 config 数组字段的长度（兼容 []any 与 []string）。
func webConfigSliceLen(v any) int {
	switch items := v.(type) {
	case []any:
		return len(items)
	case []string:
		return len(items)
	default:
		return 0
	}
}

// webSourceCron 读取源 config 的 sync.cron（缺失/空白返回空串，表示无定时仅手动触发）。
func webSourceCron(config map[string]any) string {
	syncObj, _ := config["sync"].(map[string]any)
	if syncObj == nil {
		return ""
	}
	raw, _ := syncObj["cron"].(string)
	return strings.TrimSpace(raw)
}

// EnqueueSyncForSource 为单个 web 内容源请求一次源级 source_sync 同步，
// 是 EnqueueSync 的源级同构入口（Meta Scheduler 定时派发与手动触发共用）。
//
// 通过 store.RequestSourceSyncForSource 在源行锁定事务内幂等去重（spec §9.1 同源串行）：
//   - 源级 force latch = old OR options.Force，"粘住"直到 worker 消费；
//   - 该源已有 pending/running 的源级 source_sync Job 时复用（created=false），不重复入队；
//   - 否则新建 Job（created=true；行上标注 fetch.connection_id 供 connection 限流统计），
//     由本方法按源推导 Timeout 后入队 asynq。
//
// 入队失败时把 Job 标记失败但保留 latch 与到期状态（next_sync_at 未推进），
// 由 Meta Scheduler 在下一个 Tick 周期重新派发。
func (s *SourceSyncService) EnqueueSyncForSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, options SyncOptions) (*model.Job, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id/knowledge_base_id/source_id 不能为空", domainerrors.ErrValidation)
	}
	if s.store == nil || s.queue == nil {
		return nil, fmt.Errorf("%w: 来源同步依赖未配置", domainerrors.ErrValidation)
	}

	source, err := s.store.GetSyncKbSource(ctx, workspaceID, kbID, sourceID)
	if err != nil {
		return nil, fmt.Errorf("读取内容源失败: %w", err)
	}
	if source.Kind != value.KbSourceKindWeb {
		return nil, fmt.Errorf("%w: 内容源种类 %q 不支持 web 调度", domainerrors.ErrValidation, source.Kind)
	}

	job, created, err := s.store.RequestSourceSyncForSource(ctx, workspaceID, kbID, sourceID, options.Force)
	if err != nil {
		return nil, fmt.Errorf("请求 web 源同步失败: %w", err)
	}

	// 复用进行中 Job：不重复入队（同源串行），直接返回。
	if !created {
		s.logger.Info("复用进行中的 web 内容源同步任务",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"kb_source_id", sourceID.String(),
			"job_id", job.ID.String(),
			"force", options.Force,
		)
		return job, nil
	}

	// 按源推导一次任务超时：入队与日志共用，避免两处重复推导。
	timeout := s.webSyncTaskTimeoutFor(source)
	if err := s.enqueueSourceSyncTaskForSource(ctx, workspaceID, kbID, source, job.ID, timeout); err != nil {
		// 入队失败：Job 标记失败 + 保留 latch（与飞书 FailSourceSyncEnqueue 同语义）；
		// next_sync_at 未推进，源仍处于到期状态，下一个 Tick 自动重试派发。
		if failErr := s.store.FailSourceSyncEnqueueForSource(ctx, workspaceID, kbID, sourceID, job.ID, err.Error()); failErr != nil {
			s.logger.Error("标记源级 source_sync 入队失败状态失败",
				"workspace_id", workspaceID.String(),
				"knowledge_base_id", kbID.String(),
				"kb_source_id", sourceID.String(),
				"job_id", job.ID.String(),
				"error", failErr.Error(),
			)
		}
		return nil, fmt.Errorf("入队源级 source_sync 任务失败: %w", err)
	}

	s.logger.Info("已入队 web 内容源同步任务",
		"workspace_id", workspaceID.String(),
		"knowledge_base_id", kbID.String(),
		"kb_source_id", sourceID.String(),
		"job_id", job.ID.String(),
		"force", options.Force,
		"timeout_seconds", int(timeout.Seconds()),
	)
	return job, nil
}
