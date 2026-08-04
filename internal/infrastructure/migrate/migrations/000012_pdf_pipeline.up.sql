-- v0.7.0: MinerU PDF 解析链路。
--
-- 新增 parser_raw_markdown_key 列到 document_revisions，用于记录 MinerU 产出
-- 的原始 Markdown 在对象存储（或本地文件系统）中的 storage key，便于回溯和
-- 调试。该列不参与检索或分块，仅作为可追溯的解析产物归档。
--
-- 该列可空：非 PDF 格式或旧 revision 不填写，保持向后兼容。

ALTER TABLE document_revisions ADD COLUMN IF NOT EXISTS parser_raw_markdown_key text;
