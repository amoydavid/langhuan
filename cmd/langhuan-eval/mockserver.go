package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"os"
	"time"
)

// runMockEmbedding 启动一个确定性的 OpenAI-compatible /embeddings 服务：
// 同一文本永远返回同一向量（FNV 种子 + 归一化）。它不提供任何语义能力，
// 用途只有一个——在没有真实 Embedding API 的环境下端到端验证评测 harness
// 与被测系统全链路（此时指标值无语义意义，只验证流程与确定性）。
func runMockEmbedding(args []string) error {
	fs := flag.NewFlagSet("mock-embedding", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", "127.0.0.1:19829", "监听地址")
	dimensions := fs.Int("dimensions", 1024, "向量维度（须为琅嬛支持值之一）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dim := *dimensions
	handler := http.NewServeMux()
	handler.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data := make([]struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		}, len(request.Input))
		for index, text := range request.Input {
			data[index].Embedding = deterministicVector(text, dim)
			data[index].Index = index
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list", "model": "mock-embedding", "data": data,
			"usage": map[string]int{"prompt_tokens": 0, "total_tokens": 0},
		})
	})
	server := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	fmt.Printf("mock embedding 服务已启动：http://%s/v1/embeddings（dim=%d，Ctrl-C 退出）\n", *addr, dim)
	return server.ListenAndServe()
}

func deterministicVector(text string, dimensions int) []float64 {
	seed := fnv.New64a()
	_, _ = seed.Write([]byte(text))
	state := seed.Sum64()
	vector := make([]float64, dimensions)
	norm := 0.0
	for index := range vector {
		// xorshift64 生成确定性伪随机序列。
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		vector[index] = float64(int64(state%2001)-1000) / 1000.0
		norm += vector[index] * vector[index]
	}
	norm = math.Sqrt(norm)
	for index := range vector {
		vector[index] /= norm
	}
	return vector
}
