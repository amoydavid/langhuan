-- 回滚：移除文档导入幂等表及其索引。
DROP TABLE IF EXISTS document_ingest_idempotencies;
