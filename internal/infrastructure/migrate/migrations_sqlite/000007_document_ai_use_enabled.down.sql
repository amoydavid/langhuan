-- 000007 回滚：移除文档级 AI 政策列。
-- SQLite 早期版本不支持 DROP COLUMN，现代c.org/sqlite 驱动的 SQLite >= 3.35 支持。
ALTER TABLE documents DROP COLUMN ai_use_enabled;
