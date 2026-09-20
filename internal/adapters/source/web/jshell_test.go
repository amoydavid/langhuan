package web

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// longText 生成一段长度明确的可见正文，用于跨过 JS 空壳判定的文本阈值。
func longText(n int) string {
	return strings.Repeat("正文", n/2)
}

func wrapBody(inner string) []byte {
	return []byte("<!DOCTYPE html><html><head><title>t</title></head><body>" + inner + "</body></html>")
}

func TestIsLikelyJSShell(t *testing.T) {
	t.Parallel()

	staticPage := wrapBody("<article><p>" + longText(jsShellMaxVisibleTextChars*2) + "</p></article>")

	cases := []struct {
		name         string
		html         []byte
		extractedLen int
		want         bool
	}{
		{
			name: "静态长文页不判定为空壳",
			html: staticPage,
			want: false,
		},
		{
			name: "短文本且有空的 root 挂载点",
			html: wrapBody(`<div id="root"></div><p>加载中</p>`),
			want: true,
		},
		{
			name: "空的 app 挂载点（含空白）",
			html: wrapBody(`<div id="app"> </div>`),
			want: true,
		},
		{
			name: "空的 __next 挂载点",
			html: wrapBody(`<div id="__next"></div>`),
			want: true,
		},
		{
			name: "空挂载点但内含空元素也算空",
			html: wrapBody(`<div id="root"><div><span></span></div></div>`),
			want: true,
		},
		{
			name: "挂载点内有正文不算空壳",
			html: wrapBody(`<div id="root">` + longText(jsShellMaxVisibleTextChars*2) + `</div>`),
			want: false,
		},
		{
			name: "可见文本达阈值不判定为空壳",
			html: wrapBody(`<div id="root"></div><p>` + longText(jsShellMaxVisibleTextChars+20) + `</p>`),
			want: false,
		},
		{
			name: "正文几乎全在 noscript 中",
			html: wrapBody(`<div id="x"><noscript><p>` + longText(jsShellMaxVisibleTextChars+20) + `</p></noscript></div>`),
			want: true,
		},
		{
			name: "noscript 内容不足阈值不判定",
			html: wrapBody(`<noscript><p>请开启 JavaScript</p></noscript>`),
			want: false,
		},
		{
			name:         "已抽取到足够正文时不再判定为空壳",
			html:         wrapBody(`<div id="root"></div>`),
			extractedLen: jsShellMaxVisibleTextChars,
			want:         false,
		},
		{
			name:         "抽取字数不足时仍判定",
			html:         wrapBody(`<div id="root"></div>`),
			extractedLen: jsShellMaxVisibleTextChars - 1,
			want:         true,
		},
		{
			name: "script 文本不计入可见文本",
			html: wrapBody(`<div id="root"></div><script>` + longText(jsShellMaxVisibleTextChars*2) + `</script>`),
			want: true,
		},
		{
			name: "style 文本不计入可见文本",
			html: wrapBody(`<div id="root"></div><style>` + longText(jsShellMaxVisibleTextChars*2) + `</style>`),
			want: true,
		},
		{
			name: "大小写不同的挂载点 id 不算命中",
			html: wrapBody(`<div id="ROOT"></div><p>加载中</p>`),
			want: false,
		},
		{
			name: "短文本无挂载点无 noscript 不判定",
			html: wrapBody(`<p>页面不存在</p>`),
			want: false,
		},
		{
			name: "空 HTML 不判定",
			html: nil,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, isLikelyJSShell(tc.html, tc.extractedLen))
		})
	}
}
