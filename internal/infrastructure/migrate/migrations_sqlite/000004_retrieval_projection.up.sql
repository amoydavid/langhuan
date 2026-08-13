-- SQLite 检索投影：向量存储 + 全文检索（spec §7.2、§8.2）。
-- retrieval_entries 不含 embedding/fts_document 列（PG 用 halfvec/tsvector），
-- SQLite 用本迁移的两个独立表承载，避免污染业务表。

-- retrieval_embeddings: 向量存储。embedding 为 float32 little-endian BLOB；
-- 检索用 vec_distance_cosine 精确计算（sqlite-vec 暴力扫描，单机数据量为精确解，
-- 召回率 100%，spec §7.3）。scope 索引让 WHERE 过滤后扫描限定在当前 Generation。
CREATE TABLE retrieval_embeddings (
    entry_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    index_generation_id TEXT NOT NULL,
    dimension INTEGER NOT NULL CHECK (dimension IN (798,1024,2048,3584)),
    embedding BLOB NOT NULL,
    FOREIGN KEY (workspace_id, entry_id)
        REFERENCES retrieval_entries(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_retrieval_embeddings_scope
    ON retrieval_embeddings(workspace_id, knowledge_base_id, index_generation_id, dimension);

-- retrieval_fts: 全文检索投影（FTS5 虚拟表，独立可显式维护，spec §8.2）。
-- token 由应用层 gse 分词后写入 search_content_tokenized；
-- entry_id UNINDEXED 不参与全文匹配，仅用于 JOIN 回 retrieval_entries。
-- 查询前 query 同样分词，逐 token 双引号引用 + AND 组合，模拟 PG plainto_tsquery
-- 的 plain-text AND 语义并阻止 FTS5 操作符注入。
CREATE VIRTUAL TABLE retrieval_fts USING fts5(
    entry_id UNINDEXED,
    search_content_tokenized,
    tokenize = 'unicode61 remove_diacritics 2'
);
