-- 000007_document_ai_use_enabled: 文档级 AI 政策（C02），与 PG 000029 同语义。
ALTER TABLE documents ADD COLUMN ai_use_enabled boolean NOT NULL DEFAULT TRUE;
