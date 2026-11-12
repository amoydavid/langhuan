-- 000029_document_ai_use_enabled 回滚：移除文档级 AI 政策列。

ALTER TABLE documents
    DROP COLUMN IF EXISTS ai_use_enabled;
