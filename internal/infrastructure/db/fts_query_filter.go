package db

import "unicode"

// FTS 查询侧 token 过滤（spec：docs/superpowers/specs/2026-08-24-retrieval-eval-design.md §12）。
//
// 背景：FTS 查询对 query 的全部 token 做 AND 匹配。中文疑问句会被切出
// "哪些/？/有"这类疑问词与虚词，它们几乎不出现在文档正文里，导致 AND
// 永不满足、FTS 通道对疑问句零召回（MIRACL-zh 200 query 实测 fts_only
// recall@10=0）。这里在查询侧过滤掉无检索价值的 token，只对实义词 AND：
// 关键词型 query（文件名、术语、编号）不受影响。
//
// 词表刻意保守：宁可漏过滤（损失少量召回），不可误杀实义词（伤关键词
// 查询的精确召回）。需要扩充时改这里并跑 make eval 对比。

// cjkSingleCharStopwords 是单字虚词/助词：作为 token 时无文档区分度。
var cjkSingleCharStopwords = map[rune]struct{}{
	'的': {}, '有': {}, '是': {}, '在': {}, '了': {}, '和': {}, '与': {}, '或': {},
	'等': {}, '吗': {}, '呢': {}, '吧': {}, '啊': {}, '呀': {}, '哦': {}, '嗯': {},
	'就': {}, '也': {}, '都': {}, '还': {}, '很': {}, '被': {}, '把': {}, '让': {},
	'给': {}, '从': {}, '对': {}, '向': {}, '以': {}, '于': {}, '为': {}, '而': {},
	'则': {}, '之': {}, '其': {}, '此': {}, '该': {}, '各': {}, '每': {}, '某': {},
	'又': {}, '再': {}, '才': {}, '只': {}, '及': {}, '跟': {}, '比': {}, '最': {},
}

// queryStopwords 是疑问词与口语填充词：出现在提问里、几乎不出现在文档正文。
var queryStopwords = map[string]struct{}{
	"什么": {}, "怎么": {}, "怎样": {}, "怎么样": {}, "怎么办": {},
	"哪些": {}, "哪个": {}, "哪位": {}, "哪里": {}, "哪儿": {},
	"多少": {}, "几个": {}, "如何": {}, "为何": {}, "为什么": {},
	"是不是": {}, "有没有": {}, "能不能": {}, "可不可以": {},
	"请问": {}, "一下": {}, "我想": {}, "我想问": {}, "想知道": {},
	"告诉我": {}, "介绍下": {}, "介绍一下": {}, "帮我": {}, "麻烦": {},
	"what": {}, "how": {}, "why": {}, "who": {}, "which": {}, "the": {}, "a": {}, "an": {}, "of": {}, "to": {}, "is": {}, "are": {},
}

// FilterFTSQueryTokens 过滤 query token 序列，保留参与 AND 匹配的实义词：
// 去掉纯标点/符号 token、单字虚词与疑问/填充词。全部被过滤时返回 nil，
// 调用方据此短路（等价于"无有效检索词"）。
func FilterFTSQueryTokens(tokens []string) []string {
	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if !hasSearchableRune(token) {
			continue // 纯标点/符号
		}
		if isStopwordToken(token) {
			continue
		}
		filtered = append(filtered, token)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// hasSearchableRune 判断 token 是否含字母、数字或 CJK 字符。
func hasSearchableRune(token string) bool {
	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// isStopwordToken 判断 token 是否命中停用词表（含单字虚词集合）。
func isStopwordToken(token string) bool {
	if _, ok := queryStopwords[lowerASCII(token)]; ok {
		return true
	}
	runes := []rune(token)
	if len(runes) == 1 {
		if _, ok := cjkSingleCharStopwords[runes[0]]; ok {
			return true
		}
	}
	return false
}

func lowerASCII(token string) string {
	out := []byte(token)
	changed := false
	for i, b := range out {
		if b >= 'A' && b <= 'Z' {
			out[i] = b + ('a' - 'A')
			changed = true
		}
	}
	if !changed {
		return token
	}
	return string(out)
}
