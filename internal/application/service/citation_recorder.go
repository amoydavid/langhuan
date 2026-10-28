package service

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dajee/langhuan/internal/domain/model"
)

// telemetryFlushTimeout 是单批落库的硬超时；具名常量而非字面量（AGENTS 5.8）。
const telemetryFlushTimeout = 10 * time.Second

// TelemetryStore 持久化检索遥测（v1.4.0）。由 infrastructure/db 实现；
// 写入失败不应阻塞检索——CitationRecorder 会吞掉并记录错误。
type TelemetryStore interface {
	// InsertCitations 批量写入引用行；(search_run_id, chunk_revision_id) 冲突跳过。
	InsertCitations(context.Context, []model.SearchCitation) error
	// InsertQueryLogEntries 批量写入 opt-in 查询原文行（投递侧已门控）。
	InsertQueryLogEntries(context.Context, []model.SearchQueryLogEntry) error
}

// CitationRecorderConfig 是引用遥测异步写入的进程级参数（config.yaml insights 小节）。
type CitationRecorderConfig struct {
	// BufferSize 是进程内有界缓冲的最大批位数；缓冲满时丢弃新批并计数。
	BufferSize int
	// FlushInterval 是批量落库的最长等待间隔。
	FlushInterval time.Duration
	// FlushBatchSize 是单批落库的最大行数（单批 INSERT 行数上限）。
	FlushBatchSize int
}

// CitationDropObserver 接收丢批事件（reason: buffer_full | recorder_stopped），
// 供装配层接到指标（OTel counter）；nil 时仅保留结构化日志。
type CitationDropObserver func(reason string)

// telemetryBatch 是一次投递的原子单元：同一次检索的引用行与（可选）查询原文
// 同批落库，保证明文与引用来自同一次 run。
type telemetryBatch struct {
	citations []model.SearchCitation
	queryLogs []model.SearchQueryLogEntry
}

// CitationRecorder 把检索完成处的引用遥测异步批量落库（spec §6）：
// 进程内有界 buffered channel + 后台 flusher；缓冲满丢批 + 计数 + 结构化日志，
// 任何拥塞都不得反压检索路径。明文门控在投递侧（RecordCitations 调用方）完成，
// recorder 不感知开关。
type CitationRecorder struct {
	store        TelemetryStore
	logger       *slog.Logger
	cfg          CitationRecorderConfig
	now          func() time.Time
	ch           chan telemetryBatch
	stop         chan struct{}
	done         chan struct{}
	dropped      atomic.Int64
	flushing     sync.Mutex
	closeOnce    sync.Once
	dropObserver atomic.Pointer[CitationDropObserver]
}

// SetDropObserver 延迟绑定丢批观察者（装配顺序晚于构造时调用）。
func (r *CitationRecorder) SetDropObserver(observer CitationDropObserver) {
	if r == nil || observer == nil {
		return
	}
	r.dropObserver.Store(&observer)
}

// notifyDrop 触发已绑定的观察者（未绑定为 no-op）。
func (r *CitationRecorder) notifyDrop(reason string) {
	if observer := r.dropObserver.Load(); observer != nil {
		(*observer)(reason)
	}
}

// NewCitationRecorder 构造并启动后台 flusher。cfg 非法值兜底为保守默认。
// 丢批观察者经 SetDropObserver 延迟绑定（metrics 实例在装配顺序上晚于本构造）。
func NewCitationRecorder(store TelemetryStore, cfg CitationRecorderConfig, logger *slog.Logger) *CitationRecorder {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 1024
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 500 * time.Millisecond
	}
	if cfg.FlushBatchSize <= 0 {
		cfg.FlushBatchSize = 200
	}
	if logger == nil {
		logger = slog.Default()
	}
	r := &CitationRecorder{
		store:  store,
		logger: logger,
		cfg:    cfg,
		now:    time.Now,
		ch:     make(chan telemetryBatch, cfg.BufferSize),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go r.run()
	return r
}

// RecordCitations 非阻塞投递一批遥测（引用行 + 可选查询原文）。
// 缓冲满时丢弃本批并计数——遥测的价值在趋势不在单条。
// store 为 nil（遥测未装配）时为 no-op。
func (r *CitationRecorder) RecordCitations(citations []model.SearchCitation, queryLogs []model.SearchQueryLogEntry) {
	if r == nil || r.store == nil || (len(citations) == 0 && len(queryLogs) == 0) {
		return
	}
	batch := telemetryBatch{citations: citations, queryLogs: queryLogs}
	// 已停止（Close 完成）后的投递无人消费，直接计入丢弃，不留无主的缓冲批。
	select {
	case <-r.stop:
		dropped := r.dropped.Add(1)
		r.notifyDrop("recorder_stopped")
		r.logger.Warn("insights_telemetry_dropped",
			slog.String("event", "insights_telemetry_dropped"),
			slog.String("reason", "recorder_stopped"),
			slog.Int64("total_dropped_batches", dropped),
		)
		return
	default:
	}
	select {
	case r.ch <- batch:
	default:
		dropped := r.dropped.Add(1)
		r.notifyDrop("buffer_full")
		r.logger.Warn("insights_telemetry_dropped",
			slog.String("event", "insights_telemetry_dropped"),
			slog.String("reason", "buffer_full"),
			slog.Int("citation_rows", len(citations)),
			slog.Int("query_log_rows", len(queryLogs)),
			slog.Int64("total_dropped_batches", dropped),
		)
	}
}

// Dropped 返回累计丢弃的批次数。
func (r *CitationRecorder) Dropped() int64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

// Close 停止 flusher 并尽量落库缓冲中的残量；ctx 取消或超时后放弃剩余批次。
// 幂等：重复调用安全。
func (r *CitationRecorder) Close(ctx context.Context) {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		close(r.stop)
	})
	select {
	case <-r.done:
	case <-ctx.Done():
	}
}

func (r *CitationRecorder) run() {
	defer close(r.done)
	ticker := time.NewTicker(r.cfg.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			r.drain()
			return
		case <-ticker.C:
			r.drain()
		}
	}
}

// drain 取空当前缓冲并按 FlushBatchSize 分片落库。
func (r *CitationRecorder) drain() {
	r.flushing.Lock()
	defer r.flushing.Unlock()
	var citations []model.SearchCitation
	var queryLogs []model.SearchQueryLogEntry
	for {
		select {
		case batch := <-r.ch:
			citations = append(citations, batch.citations...)
			queryLogs = append(queryLogs, batch.queryLogs...)
			// 防止单次 drain 无限扩张：攒够一批上限就先落库。
			if len(citations) >= r.cfg.FlushBatchSize || len(queryLogs) >= r.cfg.FlushBatchSize {
				r.flush(citations, queryLogs)
				citations = nil
				queryLogs = nil
			}
		default:
			r.flush(citations, queryLogs)
			return
		}
	}
}

// flush 落库一批；单次 INSERT 行数按 FlushBatchSize 切片受控。
// 失败只记日志（丢批可接受，不重试不阻塞）。
// flush panic 不拖垮 flusher goroutine（AGENTS 5.8 recover 兜底）。
func (r *CitationRecorder) flush(citations []model.SearchCitation, queryLogs []model.SearchQueryLogEntry) {
	if len(citations) == 0 && len(queryLogs) == 0 {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("insights_telemetry_flush_panic",
				slog.String("event", "insights_telemetry_flush_panic"),
				slog.Any("panic", rec),
			)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
	defer cancel()
	for start := 0; start < len(citations); start += r.cfg.FlushBatchSize {
		end := start + r.cfg.FlushBatchSize
		if end > len(citations) {
			end = len(citations)
		}
		chunk := citations[start:end]
		if err := r.store.InsertCitations(ctx, chunk); err != nil {
			r.logger.Warn("insights_telemetry_flush_failed",
				slog.String("event", "insights_telemetry_flush_failed"),
				slog.String("kind", "citations"),
				slog.Int("rows", len(chunk)),
				slog.String("error", err.Error()),
			)
		}
	}
	for start := 0; start < len(queryLogs); start += r.cfg.FlushBatchSize {
		end := start + r.cfg.FlushBatchSize
		if end > len(queryLogs) {
			end = len(queryLogs)
		}
		chunk := queryLogs[start:end]
		if err := r.store.InsertQueryLogEntries(ctx, chunk); err != nil {
			r.logger.Warn("insights_telemetry_flush_failed",
				slog.String("event", "insights_telemetry_flush_failed"),
				slog.String("kind", "query_log"),
				slog.Int("rows", len(chunk)),
				slog.String("error", err.Error()),
			)
		}
	}
}
