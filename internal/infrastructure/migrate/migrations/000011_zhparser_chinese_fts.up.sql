-- v0.6.x: 引入 PostgreSQL 中文全文检索（zhparser）。
--
-- 背景：检索的 FTS 路默认使用 simple 分词器，对中文文档不做词边界切分，
-- 「机器学习」被当作单个 token，查询「机器」无法命中，中文关键词召回几乎
-- 完全依赖向量路。本迁移注册 zhparser 扩展并创建 zhparser text search
-- configuration，供默认 fts_config（knowledge_base_generation.go）与后续
-- generation 使用；扩展本体由测试/部署镜像预装（docker/postgres-test/Dockerfile），
-- 这里只负责注册与建配置，保证迁移可回滚、可重复执行。
--
-- 注意：fts_document 是物化列，本迁移不重建已有 generation 的数据；
-- 旧 generation 仍使用其 RetrievalConfig 中记录的分词器（如 simple），
-- 新建/重建 generation 后自动切换到 zhparser。

CREATE EXTENSION IF NOT EXISTS zhparser WITH SCHEMA public;

-- 幂等创建 text search configuration（PG 无 CREATE ... IF NOT EXISTS 语法）
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_ts_config c
        WHERE c.cfgname = 'zhparser'
          AND c.cfgnamespace = 'public'::regnamespace
    ) THEN
        CREATE TEXT SEARCH CONFIGURATION public.zhparser (PARSER = public.zhparser);
    END IF;
END
$$;

-- 词性映射：仅保留实义词性（名词/动词/形容词/成语/叹词/拟声词），
-- 过滤介词、助词等噪音；配置尚无任何映射时才 ADD，保证重复执行不报错。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_ts_config_map m
        JOIN pg_ts_config c ON c.oid = m.mapcfg
        WHERE c.cfgname = 'zhparser'
          AND c.cfgnamespace = 'public'::regnamespace
    ) THEN
        ALTER TEXT SEARCH CONFIGURATION public.zhparser ADD MAPPING FOR n,v,a,i,e,l WITH simple;
    END IF;
END
$$;
