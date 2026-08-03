-- 保留 zhparser 配置与扩展：Generation 的 retrieval_config 是持久化快照，
-- 回滚应用版本不会把已经保存的 fts_config=zhparser 改回 simple。删除配置会
-- 让这些 Generation 的 plainto_tsquery(?::regconfig, ...) 立即失败。
-- 与 000001 保留 vector / pgcrypto 一致，扩展对象由运维按需清理。
SELECT 1;
