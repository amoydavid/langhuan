package value

import (
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// TestNormalizeWebSourceURI 是 spec §12.1 的 URL 规范化表驱动用例：
// 跟踪参数剥离（utm_* 前缀族 / fbclid / gclid / ref）、默认端口、fragment、
// 相对/凭证 URL 拒绝，以及"剩余参数顺序与 percent-encoding 不变"的既有行为。
//
// 键名匹配为大小写不敏感：HTTP query 键本身大小写敏感，但跟踪参数实践中大小写
// 混用（UTM_Source/utm_source 常见），而既有规范化输出保留键的原始大小写，
// 因此剥离匹配做大小写不敏感；被保留的参数原样输出，不改变其大小写。
func TestNormalizeWebSourceURI(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		// —— 既有行为回归 ——
		{
			name: "scheme/host小写+去默认端口+去fragment+query顺序保持",
			raw:  "HTTPS://Example.COM:443?b=2&a=1#fragment",
			want: "https://example.com/?b=2&a=1",
		},
		{
			name: "空path补斜杠",
			raw:  "https://example.com",
			want: "https://example.com/",
		},
		{
			name: "非默认端口保留",
			raw:  "https://example.com:8443/p?gclid=x",
			want: "https://example.com:8443/p",
		},
		{
			name: "无query不变",
			raw:  "https://example.com/p",
			want: "https://example.com/p",
		},
		{
			// url.Parse 对存在但为空的 query 设置 ForceQuery，String() 会保留尾问号；
			// 这是既有行为（剥离的空 query 早返回也不改变它），此处固化。
			name: "空query保留尾问号",
			raw:  "https://example.com/p?",
			want: "https://example.com/p?",
		},
		{
			name: "fragment中的问号不属于query",
			raw:  "https://example.com/p#frag?utm_source=x",
			want: "https://example.com/p",
		},
		{name: "相对URL拒绝", raw: "/relative?utm_source=x", wantErr: true},
		{name: "无scheme拒绝", raw: "example.com/p?ref=x", wantErr: true},
		{name: "凭证URL拒绝", raw: "https://user:pass@example.com/p?ref=x", wantErr: true},
		{name: "非HTTP(S)scheme拒绝", raw: "ftp://example.com/p", wantErr: true},

		// —— 跟踪参数剥离（spec §3.3 保守名单）——
		{
			name: "utm前缀族剥离",
			raw:  "https://example.com/p?utm_source=x&utm_medium=y&utm_campaign=z&utm_term=t&utm_content=c&page=2",
			want: "https://example.com/p?page=2",
		},
		{
			name: "精确键名fbclid/gclid/ref剥离",
			raw:  "https://example.com/p?fbclid=f&gclid=g&ref=r&keep=1",
			want: "https://example.com/p?keep=1",
		},
		{
			name: "混合参数时剩余参数顺序保持不排序",
			raw:  "https://example.com/p?b=2&utm_source=x&a=1&fbclid=y",
			want: "https://example.com/p?b=2&a=1",
		},
		{
			name: "键名大小写变体剥离而保留参数大小写不变",
			raw:  "https://example.com/p?UTM_Source=x&FBCLID=y&Gclid=z&REF=w&Keep=1",
			want: "https://example.com/p?Keep=1",
		},
		{
			name: "ref不做前缀误伤仅精确匹配",
			raw:  "https://example.com/p?reference=1&referrer=2&ref=3",
			want: "https://example.com/p?reference=1&referrer=2",
		},
		{
			name: "utm_不做前缀误伤utm不带下划线保留",
			raw:  "https://example.com/p?utm=1&utm_x=2",
			want: "https://example.com/p?utm=1",
		},
		{
			name: "全部参数被剥离时query整体消失",
			raw:  "https://example.com/p?utm_source=x&fbclid=y",
			want: "https://example.com/p",
		},
		{
			name: "无值参数同样剥离",
			raw:  "https://example.com/p?utm_source&a=1",
			want: "https://example.com/p?a=1",
		},
		{
			name: "保留参数的percent-encoding与加号原样不变",
			raw:  "https://example.com/search?q=%E4%B8%AD%E6%96%87+lang&utm_term=x&n=1",
			want: "https://example.com/search?q=%E4%B8%AD%E6%96%87+lang&n=1",
		},
		{
			name: "仅跟踪参数不同的URL归一为同一身份",
			raw:  "https://example.com/p?utm_campaign=launch&b=2&fbclid=xyz&a=1",
			want: "https://example.com/p?b=2&a=1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeWebSourceURI(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeWebSourceURI(%q) 意外成功, got %q", tt.raw, got)
				}
				if !errors.Is(err, domainerrors.ErrValidation) {
					t.Fatalf("NormalizeWebSourceURI(%q) 错误 = %v, 期望包含 %v", tt.raw, err, domainerrors.ErrValidation)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeWebSourceURI(%q) 报错: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeWebSourceURI(%q) = %q, 期望 %q", tt.raw, got, tt.want)
			}
		})
	}
}
