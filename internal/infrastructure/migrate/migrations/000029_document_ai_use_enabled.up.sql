-- 000029_document_ai_use_enabled: 文档级 AI 政策（C02，LH-005 冻结合同）。
-- documents.ai_use_enabled = false 的文档在检索召回前被排除（不进入候选与
-- 最终结果）；原文阅读/资产访问不受影响（"AI 禁用"语义 = 不得用于新检索）。
-- 默认 true 保持存量行为不变。

ALTER TABLE documents
    ADD COLUMN ai_use_enabled boolean NOT NULL DEFAULT true;
