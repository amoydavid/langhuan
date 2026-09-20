package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewSourceConnectionRejectsEmptyCredentials(t *testing.T) {
	_, err := NewSourceConnection(NewSourceConnectionInput{
		WorkspaceID:           uuid.New(),
		Provider:              "feishu",
		Name:                  "主公司飞书",
		AppID:                 "cli_a1b2",
		CredentialsCiphertext: []byte{},
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("want ErrValidation for empty ciphertext, got %v", err)
	}
}

func TestNewSourceConnectionRejectsUnknownProvider(t *testing.T) {
	_, err := NewSourceConnection(NewSourceConnectionInput{
		WorkspaceID:           uuid.New(),
		Provider:              "notion",
		Name:                  "x",
		AppID:                 "cli_a1b2",
		CredentialsCiphertext: []byte("cipher"),
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("want ErrValidation for unknown provider, got %v", err)
	}
}

func TestNewSourceConnectionStoresAppIDInConfig(t *testing.T) {
	conn, err := NewSourceConnection(NewSourceConnectionInput{
		WorkspaceID:           uuid.New(),
		Provider:              " feishu ",
		Name:                  "  主公司飞书  ",
		AppID:                 "cli_a1b2",
		CredentialsCiphertext: []byte("cipher"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn.Provider != "feishu" {
		t.Fatalf("provider = %q, want feishu", conn.Provider)
	}
	if conn.Name != "主公司飞书" {
		t.Fatalf("name = %q", conn.Name)
	}
	if got := conn.Config["app_id"]; got != "cli_a1b2" {
		t.Fatalf("config.app_id = %v", got)
	}
	if conn.Status != "active" {
		t.Fatalf("status = %q", conn.Status)
	}
}

// TestNewSourceConnectionProviderMatrix 固定 spec §3.1 的 per-provider 校验矩阵：
// app_id 仅 feishu 必填；feishu/firecrawl/jina 要求凭证密文，browser 允许空密文。
func TestNewSourceConnectionProviderMatrix(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		appID       string
		ciphertext  []byte
		config      map[string]any
		wantErr     bool
		errContains string
	}{
		{name: "feishu 齐全", provider: "feishu", appID: "cli_a1b2", ciphertext: []byte("cipher")},
		{name: "feishu 缺 app_id", provider: "feishu", appID: "  ", ciphertext: []byte("cipher"), wantErr: true, errContains: "app_id"},
		{name: "feishu 缺密文", provider: "feishu", appID: "cli_a1b2", wantErr: true, errContains: "凭证密文"},
		{name: "feishu 两者皆缺", provider: "feishu", wantErr: true, errContains: "app_id"},
		{name: "firecrawl 仅密文", provider: "firecrawl", ciphertext: []byte("fc-key")},
		{name: "firecrawl 缺密文", provider: "firecrawl", wantErr: true, errContains: "凭证密文"},
		{name: "jina 仅密文", provider: "jina", ciphertext: []byte("jina-key")},
		{name: "jina 缺密文", provider: "jina", wantErr: true, errContains: "凭证密文"},
		{name: "browser 空密文（endpoint 在 config）", provider: "browser", config: map[string]any{"endpoint_url": "ws://cdp:9222"}},
		{name: "browser 可选 token", provider: "browser", ciphertext: []byte("cdp-token"), config: map[string]any{"endpoint_url": "ws://cdp:9222"}},
		{name: "firecrawl 不接受 app_id", provider: "firecrawl", appID: "cli_a1b2", ciphertext: []byte("fc-key"), wantErr: true, errContains: "app_id"},
		{name: "browser 不接受 app_id", provider: "browser", appID: "cli_a1b2", wantErr: true, errContains: "app_id"},
		{name: "provider 为空", provider: "", ciphertext: []byte("cipher"), wantErr: true, errContains: "provider"},
		{name: "provider 未知", provider: "notion", ciphertext: []byte("cipher"), wantErr: true, errContains: "provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := NewSourceConnection(NewSourceConnectionInput{
				WorkspaceID:           uuid.New(),
				Provider:              tt.provider,
				Name:                  "连接",
				AppID:                 tt.appID,
				CredentialsCiphertext: tt.ciphertext,
				Config:                tt.config,
			})
			if tt.wantErr {
				if !errors.Is(err, domainerrors.ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("err = %v, want 含 %q", err, tt.errContains)
				}
				if conn != nil {
					t.Fatalf("conn = %+v, want nil on validation error", conn)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if conn.Provider != strings.TrimSpace(tt.provider) {
				t.Fatalf("provider = %q, want %q", conn.Provider, tt.provider)
			}
			if conn.Status != "active" {
				t.Fatalf("status = %q, want active", conn.Status)
			}
			if len(conn.CredentialsCiphertext) != len(tt.ciphertext) {
				t.Fatalf("ciphertext = %v, want %v", conn.CredentialsCiphertext, tt.ciphertext)
			}
			// app_id 只属于飞书内部应用；其它 provider 不写入该键。
			if _, ok := conn.Config["app_id"]; ok != (conn.Provider == "feishu") {
				t.Fatalf("config.app_id 存在性 = %v, provider = %q", ok, conn.Provider)
			}
		})
	}
}

// TestNewSourceConnectionConfigMatrix 固定 T12 的连接 config 合同（spec §3.1）：
// endpoint_url 仅 firecrawl（可选，覆盖官方云）与 browser（必填 CDP 端点）接受，
// 键名统一 endpoint_url（T11 凭证注入按此键消费）；其余 provider 不接受任何
// config 键，未知键显式拒绝（与 app_id 同口径：拒绝优于静默丢弃）。
func TestNewSourceConnectionConfigMatrix(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		appIDConfig string
		ciphertext  []byte
		config      map[string]any
		wantErr     bool
		errContains string
		// wantEndpoint 为 nil 表示不断言 endpoint 键；非 nil 断言存储值。
		wantEndpoint *string
	}{
		{
			name:         "firecrawl 携带 endpoint_url",
			provider:     "firecrawl",
			ciphertext:   []byte("fc-key"),
			config:       map[string]any{"endpoint_url": "https://firecrawl.internal:3002"},
			wantEndpoint: ptrString("https://firecrawl.internal:3002"),
		},
		{
			name:       "firecrawl 无 config（缺省官方云）",
			provider:   "firecrawl",
			ciphertext: []byte("fc-key"),
		},
		{
			name:         "browser 携带 ws endpoint",
			provider:     "browser",
			config:       map[string]any{"endpoint_url": " ws://cdp.internal:9222 "},
			wantEndpoint: ptrString("ws://cdp.internal:9222"),
		},
		{
			name:        "browser 缺 endpoint_url 必报错",
			provider:    "browser",
			wantErr:     true,
			errContains: "endpoint_url",
		},
		{
			name:        "browser 空 endpoint_url 必报错",
			provider:    "browser",
			config:      map[string]any{"endpoint_url": "   "},
			wantErr:     true,
			errContains: "endpoint_url",
		},
		{
			name:        "browser endpoint 仅支持 ws/wss",
			provider:    "browser",
			config:      map[string]any{"endpoint_url": "http://cdp.internal:9222"},
			wantErr:     true,
			errContains: "ws/wss",
		},
		{
			name:        "browser endpoint 不可解析",
			provider:    "browser",
			config:      map[string]any{"endpoint_url": "://not-a-url"},
			wantErr:     true,
			errContains: "endpoint_url",
		},
		{
			name:        "firecrawl endpoint 仅支持 http/https",
			provider:    "firecrawl",
			ciphertext:  []byte("fc-key"),
			config:      map[string]any{"endpoint_url": "ftp://firecrawl.internal"},
			wantErr:     true,
			errContains: "http/https",
		},
		{
			name:        "feishu 不接受 config",
			provider:    "feishu",
			appIDConfig: "cli_a1b2",
			ciphertext:  []byte("cipher"),
			config:      map[string]any{"endpoint_url": "https://x"},
			wantErr:     true,
			errContains: "不支持自定义端点",
		},
		{
			name:        "jina 不接受 config（暂无端点覆盖入口）",
			provider:    "jina",
			ciphertext:  []byte("jina-key"),
			config:      map[string]any{"endpoint_url": "https://jina.internal"},
			wantErr:     true,
			errContains: "不支持自定义端点",
		},
		{
			name:        "未知 config 键显式拒绝",
			provider:    "firecrawl",
			ciphertext:  []byte("fc-key"),
			config:      map[string]any{"endpoint": "https://x"},
			wantErr:     true,
			errContains: "不支持的连接配置键",
		},
		{
			name:        "endpoint 非字符串拒绝",
			provider:    "browser",
			config:      map[string]any{"endpoint_url": 123},
			wantErr:     true,
			errContains: "字符串",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := NewSourceConnection(NewSourceConnectionInput{
				WorkspaceID:           uuid.New(),
				Provider:              tt.provider,
				Name:                  "连接",
				AppID:                 tt.appIDConfig,
				CredentialsCiphertext: tt.ciphertext,
				Config:                tt.config,
			})
			if tt.wantErr {
				if !errors.Is(err, domainerrors.ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("err = %v, want 含 %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if tt.wantEndpoint == nil {
				if _, ok := conn.Config["endpoint_url"]; ok {
					t.Fatalf("config.endpoint_url = %v, want 不存在", conn.Config["endpoint_url"])
				}
				return
			}
			if got := conn.Config["endpoint_url"]; got != *tt.wantEndpoint {
				t.Fatalf("config.endpoint_url = %v, want %q", got, *tt.wantEndpoint)
			}
		})
	}
}

func ptrString(s string) *string { return &s }

// browserConfig 供归一化测试构造 browser 连接的必填 endpoint config。
func browserConfig(endpoint string) map[string]any {
	if endpoint == "" {
		return nil
	}
	return map[string]any{"endpoint_url": endpoint}
}

// TestNewSourceConnectionNormalizesProvider 验证大小写与空白归一化对四种
// provider 一致生效。
func TestNewSourceConnectionNormalizesProvider(t *testing.T) {
	tests := []struct {
		raw             string
		want            string
		appID           string
		browserEndpoint string
	}{
		{raw: " FEISHU ", want: "feishu", appID: "cli_a1b2"},
		{raw: "Firecrawl", want: "firecrawl"},
		{raw: "JINA", want: "jina"},
		{raw: " browser ", want: "browser", browserEndpoint: "ws://cdp:9222"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			conn, err := NewSourceConnection(NewSourceConnectionInput{
				WorkspaceID:           uuid.New(),
				Provider:              tt.raw,
				Name:                  "连接",
				AppID:                 tt.appID,
				CredentialsCiphertext: []byte("cipher"),
				Config:                browserConfig(tt.browserEndpoint),
			})
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if conn.Provider != tt.want {
				t.Fatalf("provider = %q, want %q", conn.Provider, tt.want)
			}
		})
	}
}

func TestNewKnowledgeBaseWithSourceFeishuRequiresConnection(t *testing.T) {
	_, err := NewKnowledgeBaseWithSource(uuid.New(), "x", "", uuid.New(), nil, map[string]any{},
		value.SourceTypeFeishuWiki, map[string]any{"root_token": "wikcnB"}, nil)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("want ErrValidation for feishu without connection, got %v", err)
	}
}

func TestNewKnowledgeBaseWithSourceRejectsUnknownType(t *testing.T) {
	connID := uuid.New()
	_, err := NewKnowledgeBaseWithSource(uuid.New(), "x", "", uuid.New(), nil, map[string]any{},
		"unknown", map[string]any{}, &connID)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("want ErrValidation for unknown source type, got %v", err)
	}
}

func TestNewKnowledgeBaseDefaultsToUpload(t *testing.T) {
	kb, err := NewKnowledgeBase(uuid.New(), "kb", "", uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kb.SourceType != value.SourceTypeUpload {
		t.Fatalf("source_type = %q, want upload", kb.SourceType)
	}
	if kb.SourceConfig == nil {
		t.Fatal("source_config should default to empty map")
	}
}

func TestNewKnowledgeBaseWithSourceFeishuBindsConnection(t *testing.T) {
	connID := uuid.New()
	kb, err := NewKnowledgeBaseWithSource(uuid.New(), "飞书KB", "", uuid.New(), nil, map[string]any{},
		value.SourceTypeFeishuWiki, map[string]any{"root_token": "wikcnB"}, &connID)
	if err != nil {
		t.Fatal(err)
	}
	if kb.SourceType != value.SourceTypeFeishuWiki {
		t.Fatalf("source_type = %q", kb.SourceType)
	}
	if kb.SourceConnectionID == nil || *kb.SourceConnectionID != connID {
		t.Fatal("connection not bound")
	}
	if kb.SourceConfig["root_token"] != "wikcnB" {
		t.Fatal("source_config not stored")
	}
}
