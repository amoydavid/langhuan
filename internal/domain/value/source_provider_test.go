package value

import (
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestSourceProviderValidateAcceptsKnownProviders(t *testing.T) {
	tests := []SourceProvider{
		SourceProviderFeishu,
		SourceProviderFirecrawl,
		SourceProviderJina,
		SourceProviderBrowser,
	}
	for _, provider := range tests {
		t.Run(string(provider), func(t *testing.T) {
			if err := provider.Validate(); err != nil {
				t.Fatalf("Validate(%q) err = %v, want nil", provider, err)
			}
		})
	}
}

func TestSourceProviderValidateRejectsUnknownOrEmpty(t *testing.T) {
	tests := []SourceProvider{
		"",
		"   ",
		"notion",
		"firecrawl ", // 归一化由 model 构造函数负责，value 层不做裁剪
		"web",
	}
	for _, provider := range tests {
		t.Run(string(provider), func(t *testing.T) {
			if err := provider.Validate(); !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("Validate(%q) err = %v, want ErrValidation", provider, err)
			}
		})
	}
}

// TestSourceProviderFieldRequirements 固定 spec §3.1 的 per-provider 字段要求：
// app_id 仅 feishu 必填；feishu/firecrawl/jina 要求凭证密文；browser 允许空密文。
func TestSourceProviderFieldRequirements(t *testing.T) {
	tests := []struct {
		provider        SourceProvider
		wantAppID       bool
		wantCredentials bool
	}{
		{provider: SourceProviderFeishu, wantAppID: true, wantCredentials: true},
		{provider: SourceProviderFirecrawl, wantAppID: false, wantCredentials: true},
		{provider: SourceProviderJina, wantAppID: false, wantCredentials: true},
		{provider: SourceProviderBrowser, wantAppID: false, wantCredentials: false},
	}
	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			if got := tt.provider.RequiresAppID(); got != tt.wantAppID {
				t.Fatalf("RequiresAppID(%q) = %v, want %v", tt.provider, got, tt.wantAppID)
			}
			if got := tt.provider.RequiresCredentials(); got != tt.wantCredentials {
				t.Fatalf("RequiresCredentials(%q) = %v, want %v", tt.provider, got, tt.wantCredentials)
			}
		})
	}
}
