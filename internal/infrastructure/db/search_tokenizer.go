package db

// SearchTokenizer 把文本切分为检索 token 序列。
//
// 只被 SQLite retrieval 基础设施消费：FTS5 无法注册自定义 tokenizer（modernc.org/sqlite
// 不暴露该能力），因此中文分词必须在应用层完成，写入/查询 FTS5 前各调用一次。
// PG 路径不构造该依赖（PG 在 SQL 层用 to_tsvector/plainto_tsquery + zhparser）。
//
// 接口定义在使用方（db 包），gse adapter 实现；不把分词实现抬升到 application 层。
type SearchTokenizer interface {
	// Tokens 返回切分后的 token 列表。空文本返回 nil。
	Tokens(text string) []string
}
