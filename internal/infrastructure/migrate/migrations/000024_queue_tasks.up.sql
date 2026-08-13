-- queue_tasks：无 Redis 部署（PG+SQLite 队列表 / SQLite standalone）的持久化任务队列。
-- 状态机 pending → active → (deleted | retry→pending | dead)。
-- 时间列用 BIGINT Unix 毫秒（避免跨方言时间格式歧义）。
-- task_id 部分唯一索引：pending/active 期间幂等去重（与 asynq TaskID 语义一致）。
CREATE TABLE queue_tasks (
    id           BIGSERIAL PRIMARY KEY,
    task_id      TEXT    NOT NULL DEFAULT '',
    type         TEXT    NOT NULL,
    payload      BYTEA   NOT NULL,
    state        TEXT    NOT NULL DEFAULT 'pending',
    attempts     INTEGER NOT NULL DEFAULT 0,
    max_retry    INTEGER NOT NULL DEFAULT 4,
    timeout_ms   BIGINT  NOT NULL DEFAULT 0,
    scheduled_at BIGINT  NOT NULL DEFAULT 0,
    enqueued_at  BIGINT  NOT NULL DEFAULT 0,
    last_error   TEXT    NOT NULL DEFAULT '',
    dead_at      BIGINT
);

CREATE INDEX idx_queue_state_scheduled ON queue_tasks(state, scheduled_at);

CREATE UNIQUE INDEX uq_queue_task_id_live
    ON queue_tasks(task_id)
    WHERE task_id != '' AND state IN ('pending', 'active');
