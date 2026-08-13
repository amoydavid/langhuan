// Package gse 实现 db.SearchTokenizer，基于 github.com/go-ego/gse 的纯 Go 中文分词。
//
// 现代化纯 Go 中文分词（DAG + HMM/Viterbi），内嵌词典，支持 //go:embed 单文件分发。
// 构造时一次性加载词典（LoadDictEmbed），之后只读使用，并发安全（go test -race 验证）。
// PG 路径不构造本 adapter（PG 用 zhparser 在 SQL 层分词）。
package gse

import (
	"strings"

	gseseg "github.com/go-ego/gse"

	"github.com/dajee/langhuan/internal/infrastructure/db"
)

// 编译期断言：Segmenter 实现 db.SearchTokenizer。
var _ db.SearchTokenizer = (*Segmenter)(nil)

// Segmenter 是基于 gse 的中文分词器，构造后只读。
type Segmenter struct {
	seg *gseseg.Segmenter
}

// New 加载内嵌词典并构造分词器。词典加载较重（数十毫秒），整个进程构造一次即可。
func New() (*Segmenter, error) {
	seg, err := gseseg.NewEmbed()
	if err != nil {
		return nil, err
	}
	return &Segmenter{seg: &seg}, nil
}

// Tokens 把文本切分为 token 列表，去除空白 token。
// 写入与查询共用同一分词器与切分模式，保证 FTS5 索引与查询的 token 一致。
func (s *Segmenter) Tokens(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	words := s.seg.Cut(text, true)
	tokens := words[:0]
	for _, w := range words {
		if strings.TrimSpace(w) != "" {
			tokens = append(tokens, w)
		}
	}
	return tokens
}
