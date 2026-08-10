package main

import (
	"context"
	"fmt"
	"time"

	"github.com/dajee/langhuan/internal/adapters/queue/asynq"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	langhttp "github.com/dajee/langhuan/internal/interfaces/http"
)

// readinessChecker 探活 PostgreSQL、Redis 与 asynq 队列积压。
type readinessChecker struct {
	db                dbPinger
	redis             redisPinger
	inspector         *asynq.QueueInspector
	queuePendingLimit int
}

// dbPinger 探活数据库（*gorm.DB 实现）。
type dbPinger interface {
	PingWithTimeout(ctx context.Context) error
}

// redisPinger 探活 Redis（*redis.Client 实现）。
type redisPinger interface {
	PingWithTimeout(ctx context.Context) error
}

func newReadinessChecker(db dbPinger, redis redisPinger, inspector *asynq.QueueInspector, cfg config.ObservabilityConfig) *readinessChecker {
	return &readinessChecker{
		db:                db,
		redis:             redis,
		inspector:         inspector,
		queuePendingLimit: cfg.Readiness.QueuePendingThreshold,
	}
}

func (r *readinessChecker) Check(ctx context.Context) langhttp.ReadinessReport {
	checks := make(map[string]langhttp.ReadinessCheck, 3)
	ready := true

	// PostgreSQL
	if r.db != nil {
		if err := r.db.PingWithTimeout(ctx); err != nil {
			checks["database"] = langhttp.ReadinessCheck{OK: false, Message: err.Error()}
			ready = false
		} else {
			checks["database"] = langhttp.ReadinessCheck{OK: true}
		}
	}

	// Redis
	if r.redis != nil {
		if err := r.redis.PingWithTimeout(ctx); err != nil {
			checks["redis"] = langhttp.ReadinessCheck{OK: false, Message: err.Error()}
			ready = false
		} else {
			checks["redis"] = langhttp.ReadinessCheck{OK: true}
		}
	}

	// asynq 队列积压（可选，limit<=0 跳过）
	if r.inspector != nil && r.queuePendingLimit > 0 {
		pending, err := r.inspector.TotalPending(ctx)
		if err != nil {
			checks["queue"] = langhttp.ReadinessCheck{OK: false, Message: err.Error()}
			ready = false
		} else if pending > r.queuePendingLimit {
			checks["queue"] = langhttp.ReadinessCheck{OK: false, Message: fmt.Sprintf("pending %d 超过阈值 %d", pending, r.queuePendingLimit)}
			ready = false
		} else {
			checks["queue"] = langhttp.ReadinessCheck{OK: true}
		}
	}

	return langhttp.ReadinessReport{Ready: ready, Checks: checks}
}

// pingDB 用 gorm 探活 PostgreSQL（2s 超时）。
type gormPinger struct {
	ping func(ctx context.Context) error
}

func (g gormPinger) PingWithTimeout(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return g.ping(cctx)
}

// pingRedisValue 用 redis.Client 探活（2s 超时）。
type redisPingerImpl struct {
	ping func(ctx context.Context) error
}

func (r redisPingerImpl) PingWithTimeout(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return r.ping(cctx)
}
