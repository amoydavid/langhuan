package mineru

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPollParsesExtractResultDone 验证 MinerU 批量查询接口的真实响应结构：
// 状态在 data.extract_result[] 数组里，而非 data 顶层。
func TestPollParsesExtractResultDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v4/extract-results/batch/batch-001") {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"code": 0,
			"msg": "ok",
			"data": {
				"batch_id": "batch-001",
				"extract_result": [{
					"file_name": "document.pdf",
					"state": "done",
					"err_msg": "",
					"full_zip_url": "https://cdn.example.com/result.zip"
				}]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		Token:   "test-token",
	})
	result, err := client.Poll(context.Background(), "batch-001")
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if result.Status != TaskStatusSucceeded {
		t.Fatalf("Status = %v, want succeeded", result.Status)
	}
	if result.FullResultURL != "https://cdn.example.com/result.zip" {
		t.Fatalf("FullResultURL = %q", result.FullResultURL)
	}
}

func TestPollParsesExtractResultRunning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"code": 0,
			"msg": "ok",
			"data": {
				"batch_id": "batch-002",
				"extract_result": [{
					"file_name": "document.pdf",
					"state": "running",
					"err_msg": ""
				}]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, Token: "t"})
	result, err := client.Poll(context.Background(), "batch-002")
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if result.Status != TaskStatusRunning {
		t.Fatalf("Status = %v, want running", result.Status)
	}
}

func TestPollParsesExtractResultFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"code": 0,
			"msg": "ok",
			"data": {
				"batch_id": "batch-003",
				"extract_result": [{
					"file_name": "document.pdf",
					"state": "failed",
					"err_msg": "解析失败: 文件损坏",
					"full_zip_url": ""
				}]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, Token: "t"})
	result, err := client.Poll(context.Background(), "batch-003")
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if result.Status != TaskStatusFailed {
		t.Fatalf("Status = %v, want failed", result.Status)
	}
	if result.ErrorCode != "mineru_parse_failed" {
		t.Fatalf("ErrorCode = %q", result.ErrorCode)
	}
	if result.ErrorMessage != "解析失败: 文件损坏" {
		t.Fatalf("ErrorMessage = %q", result.ErrorMessage)
	}
}

func TestPollEmptyExtractResultIsRunning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"msg":"ok","data":{"batch_id":"batch-004","extract_result":[]}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, Token: "t"})
	result, err := client.Poll(context.Background(), "batch-004")
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if result.Status != TaskStatusRunning {
		t.Fatalf("Status = %v, want running (empty result)", result.Status)
	}
}

func TestPollMixedResultAnyFailedWins(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"code": 0,
			"msg": "ok",
			"data": {
				"batch_id": "batch-005",
				"extract_result": [
					{"file_name": "a.pdf", "state": "done", "err_msg": "", "full_zip_url": "https://cdn/a.zip"},
					{"file_name": "b.pdf", "state": "failed", "err_msg": "解析失败", "full_zip_url": ""}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, Token: "t"})
	result, err := client.Poll(context.Background(), "batch-005")
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if result.Status != TaskStatusFailed {
		t.Fatalf("Status = %v, want failed (any file failed)", result.Status)
	}
}
