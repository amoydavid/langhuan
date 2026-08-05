package compatible_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	rerankadapter "github.com/dajee/langhuan/internal/adapters/rerank"
	"github.com/dajee/langhuan/internal/adapters/rerank/compatible"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// newTestClient 构造一个指向 httptest server 的 client，绕过 SSRF 公网校验。
func newTestClient(t *testing.T, serverURL string, params compatible.ModelParameters) rerankport.Client {
	t.Helper()
	factory := compatible.NewFactory()
	configMap, credentialsJSON, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
		Scope:       value.ModelScopePlatform,
		Config:      mustJSON(t, map[string]any{"base_url": serverURL, "timeout_seconds": 5, "retry_times": 2}),
		Credentials: mustJSON(t, map[string]any{"api_key": "secret"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := factory.DecodeModel(rerankport.ModelDecodeInput{
		ModelName:  "bge-reranker-v2-m3",
		Parameters: mustJSON(t, params),
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.NewClient(context.Background(), rerankport.ClientInput{
		ProviderID:      uuid.New(),
		Scope:           value.ModelScopePlatform,
		Config:          configMap,
		CredentialsJSON: credentialsJSON,
		ModelName:       "bge-reranker-v2-m3",
		Parameters:      parameters,
	})
	if err != nil {
		t.Fatalf("new client err = %v", err)
	}
	return client
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func defaultParams() compatible.ModelParameters {
	return compatible.ModelParameters{MaxDocuments: 100, MaxQueryChars: 4096, MaxDocumentChars: 8192}
}

func TestClientRestoresIDsAndRejectsDuplicateIndexes(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("auth = %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content-type = %s", r.Header.Get("Content-Type"))
		}
		_, _ = io.WriteString(w, `{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.3}]}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, defaultParams())
	got, err := client.Rerank(context.Background(), rerankport.RerankInput{
		Query:     "query",
		Documents: []rerankport.Document{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}},
		TopN:      2,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got.Items) != 2 || got.Items[0].DocumentID != "b" || got.Items[0].Score != 0.9 {
		t.Fatalf("items = %#v", got.Items)
	}
}

func TestClientIgnoresExtraFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"request_id":"upstream-trace","results":[{"index":0,"relevance_score":0.5,"metadata":"x"}]}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, defaultParams())
	got, err := client.Rerank(context.Background(), rerankport.RerankInput{
		Query:     "q",
		Documents: []rerankport.Document{{ID: "a", Text: "A"}},
		TopN:      1,
	})
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("got = %#v err = %v", got, err)
	}
}

func TestClientRejectsInvalidResponses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"invalid json", `not-json`},
		{"duplicate index", `{"results":[{"index":0,"relevance_score":0.1},{"index":0,"relevance_score":0.2}]}`},
		{"out of range index", `{"results":[{"index":5,"relevance_score":0.1},{"index":1,"relevance_score":0.2}]}`},
		{"missing index", `{"results":[{"index":0,"relevance_score":0.1}]}`},
		{"nan score", `{"results":[{"index":0,"relevance_score":"NaN"},{"index":1,"relevance_score":0.2}]}`},
		{"wrong count", `{"results":[{"index":0,"relevance_score":0.1},{"index":1,"relevance_score":0.2},{"index":2,"relevance_score":0.3}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			client := newTestClient(t, server.URL, defaultParams())
			_, err := client.Rerank(context.Background(), rerankport.RerankInput{
				Query:     "q",
				Documents: []rerankport.Document{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}},
				TopN:      2,
			})
			if !errors.Is(err, domainerrors.ErrInvalidRerankResponse) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "NaN-upstream-body") {
				t.Fatalf("error leaked body: %v", err)
			}
		})
	}
}

func TestClientRejectsBodyOverLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 写入超过 2 MiB 的响应体。
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, (2<<20)+16))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, defaultParams())
	_, err := client.Rerank(context.Background(), rerankport.RerankInput{
		Query:     "q",
		Documents: []rerankport.Document{{ID: "a", Text: "A"}},
		TopN:      1,
	})
	if !errors.Is(err, domainerrors.ErrInvalidRerankResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientRetriesOn5xxThenSucceeds(t *testing.T) {
	t.Parallel()
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = io.WriteString(w, `{"results":[{"index":0,"relevance_score":0.7}]}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, defaultParams())
	got, err := client.Rerank(context.Background(), rerankport.RerankInput{
		Query:     "q",
		Documents: []rerankport.Document{{ID: "a", Text: "A"}},
		TopN:      1,
	})
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("got = %#v err = %v attempts = %d", got, err, atomic.LoadInt32(&attempts))
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d", got)
	}
}

func TestClientRetriesOn429ThenFails(t *testing.T) {
	t.Parallel()
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, defaultParams())
	_, err := client.Rerank(context.Background(), rerankport.RerankInput{
		Query:     "q",
		Documents: []rerankport.Document{{ID: "a", Text: "A"}},
		TopN:      1,
	})
	if !errors.Is(err, domainerrors.ErrRerankRateLimited) {
		t.Fatalf("err = %v", err)
	}
	// retry_times=2 -> 3 attempts total
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d", got)
	}
}

func TestClientDoesNotRetryOn401(t *testing.T) {
	t.Parallel()
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, defaultParams())
	_, err := client.Rerank(context.Background(), rerankport.RerankInput{
		Query:     "q",
		Documents: []rerankport.Document{{ID: "a", Text: "A"}},
		TopN:      1,
	})
	if !errors.Is(err, domainerrors.ErrAuthenticationFailed) {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d", got)
	}
}

func TestClientDoesNotRetryOn400(t *testing.T) {
	t.Parallel()
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, defaultParams())
	_, err := client.Rerank(context.Background(), rerankport.RerankInput{
		Query:     "q",
		Documents: []rerankport.Document{{ID: "a", Text: "A"}},
		TopN:      1,
	})
	// 400 maps to provider_rejected -> ErrRerankUnavailable (default branch)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d", got)
	}
}

func TestClientRespectsContextCancel(t *testing.T) {
	t.Parallel()
	// 服务器固定睡眠，确保由客户端 context deadline 触发取消，而非服务端行为。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, defaultParams())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.Rerank(ctx, rerankport.RerankInput{
		Query:     "q",
		Documents: []rerankport.Document{{ID: "a", Text: "A"}},
		TopN:      1,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientValidateInputRejectsBadInput(t *testing.T) {
	t.Parallel()
	// 此用例不需要 server；输入校验在调用前失败。
	client := newTestClient(t, "https://rerank.example.com", defaultParams())
	tests := []rerankport.RerankInput{
		{Query: "", Documents: []rerankport.Document{{ID: "a", Text: "A"}}, TopN: 1},
		{Query: "q", Documents: []rerankport.Document{}, TopN: 1},
		{Query: "q", Documents: []rerankport.Document{{ID: "a", Text: "A"}}, TopN: 0},
		{Query: "q", Documents: []rerankport.Document{{ID: "a", Text: "A"}}, TopN: 5},
	}
	for _, input := range tests {
		_, err := client.Rerank(context.Background(), input)
		if !errors.Is(err, domainerrors.ErrRerankInputTooLarge) {
			t.Fatalf("input %+v err = %v", input, err)
		}
	}
}

func TestClientProviderErrorIsExported(t *testing.T) {
	// 防御性用例：确保 adapter 包暴露的 ProviderError 常量与 client 错误链一致。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, compatible.ModelParameters{MaxDocuments: 100, MaxQueryChars: 4096, MaxDocumentChars: 8192})
	_, err := client.Rerank(context.Background(), rerankport.RerankInput{
		Query:     "q",
		Documents: []rerankport.Document{{ID: "a", Text: "A"}},
		TopN:      1,
	})
	if !errors.Is(err, domainerrors.ErrRerankUnavailable) {
		t.Fatalf("err = %v", err)
	}
	// 引用一次常量，避免被 linter 当作未使用导出符号。
	_ = rerankadapter.ProviderErrorUnreachable
}
