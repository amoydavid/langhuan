package gse

import (
	"strings"
	"sync"
	"testing"
)

func TestSegmenterChinese(t *testing.T) {
	seg, err := New()
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	tokens := seg.Tokens("退款将在三个工作日内到账")
	if len(tokens) == 0 {
		t.Fatal("应切出非空 token")
	}
	joined := strings.Join(tokens, " ")
	if !strings.Contains(joined, "退款") {
		t.Logf("tokens = %v", tokens)
	}
}

func TestSegmenterEmpty(t *testing.T) {
	seg, _ := New()
	if got := seg.Tokens(""); got != nil {
		t.Fatalf("空文本应返回 nil, got %v", got)
	}
	if got := seg.Tokens("   "); got != nil {
		t.Fatalf("纯空白应返回 nil, got %v", got)
	}
}

func TestSegmenterEnglishMixed(t *testing.T) {
	seg, _ := New()
	tokens := seg.Tokens("RAG 检索增强生成 embedding 向量")
	if len(tokens) == 0 {
		t.Fatal("中英混合应切出 token")
	}
}

func TestSegmenterConcurrent(t *testing.T) {
	seg, _ := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = seg.Tokens("并发分词测试，琅嬛知识检索服务。")
		}()
	}
	wg.Wait()
}
