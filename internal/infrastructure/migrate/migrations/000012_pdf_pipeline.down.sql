-- 回滚 v0.7.0: 移除 document_revisions.parser_raw_markdown_key 列。

ALTER TABLE document_revisions DROP COLUMN IF EXISTS parser_raw_markdown_key;
