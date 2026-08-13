-- queue_tasks：无 Redis 部署（PG+SQLite 队列表 / SQLite standalone）的持久化任务队列。
-- 与 PG 000024_queue_tasks 语义等价（SQLite 方言：INTEGER AUTOINCREMENT / BLOB）。
CREATE TABLE queue_tasks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id      TEXT    NOT NULL DEFAULT '',
    type         TEXT    NOT NULL,
    payload      BLOB    NOT NULL,
    state        TEXT    NOT NULL DEFAULT 'pending',
    attempts     INTEGER NOT NULL DEFAULT 0,
    max_retry    INTEGER NOT NULL DEFAULT 4,
    timeout_ms   INTEGER NOT NULL DEFAULT 0,
    scheduled_at INTEGER NOT NULL DEFAULT 0,
    enqueued_at  INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT    NOT NULL DEFAULT '',
    dead_at      INTEGER
);

CREATE INDEX IF NOT EXISTS idx_queue_state_scheduled ON queue_tasks(state, scheduled_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_queue_task_id_live
    ON queue_tasks(task_id)
    WHERE task_id != '' AND state IN ('pending', 'active');
