package feishu

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// TestParseURL 覆盖飞书分享链接/裸 token 的各类解析场景。
//
// 注意：ParseURL 仅把以 http:// / https:// 开头的输入当作 URL 解析；
// 不带 scheme 的输入（即便形如 feishu.cn/wiki/xxx）会被整体当作裸 token。
func TestParseURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantKind  string
		wantToken string
		wantErr   bool
	}{
		{
			name:      "wiki 链接（https + 子域）",
			input:     "https://xxx.feishu.cn/wiki/wikcnB123",
			wantKind:  sourceport.SyncRootWikiNode,
			wantToken: "wikcnB123",
		},
		{
			name:      "drive folder 链接（https + 子域）",
			input:     "https://xxx.feishu.cn/drive/folder/fldcnX",
			wantKind:  sourceport.SyncRootDriveFolder,
			wantToken: "fldcnX",
		},
		{
			name:      "docx 链接（首版按 wiki_node 处理）",
			input:     "https://tenant.feishu.cn/docx/doxcnY",
			wantKind:  sourceport.SyncRootWikiNode,
			wantToken: "doxcnY",
		},
		{
			name:      "裸 token（无法判别类型，默认 wiki_node）",
			input:     "wikcnB",
			wantKind:  sourceport.SyncRootWikiNode,
			wantToken: "wikcnB",
		},
		{
			name:    "非法 host（非飞书域名）应报错",
			input:   "https://example.com/wiki/x",
			wantErr: true,
		},
		{
			name:    "空输入应报错",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := ParseURL(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKind, root.Kind)
			assert.Equal(t, tt.wantToken, root.Token)
		})
	}
}

// TestParseURLSchemelessInputTreatedAsBareToken 验证不带 scheme 的输入被当作裸 token：
// 整串作为 Token，Kind 默认 wiki_node。这是 ParseURL 当前的实际行为。
func TestParseURLSchemelessInputTreatedAsBareToken(t *testing.T) {
	root, err := ParseURL("feishu.cn/wiki/wikcnB123")
	require.NoError(t, err)
	assert.Equal(t, sourceport.SyncRootWikiNode, root.Kind)
	// 整串原样作为 token（未做 path 拆分）。
	assert.Equal(t, "feishu.cn/wiki/wikcnB123", root.Token)
}

// TestParseURLHttpsWikiRootPath 验证 https scheme 下 wiki path 段解析。
func TestParseURLHttpsWikiRootPath(t *testing.T) {
	root, err := ParseURL("https://inner.feishu.cn/wiki/wikcnHttps")
	require.NoError(t, err)
	assert.Equal(t, sourceport.SyncRootWikiNode, root.Kind)
	assert.Equal(t, "wikcnHttps", root.Token)
}

// TestParseURLDriveFolderSubpath 验证 /drive/folder/<token> 路径段结构。
func TestParseURLDriveFolderSubpath(t *testing.T) {
	root, err := ParseURL("https://tenant.feishu.cn/drive/folder/fldcnSub")
	require.NoError(t, err)
	assert.Equal(t, sourceport.SyncRootDriveFolder, root.Kind)
	assert.Equal(t, "fldcnSub", root.Token)
}

// TestParseURLLarksuiteHost 验证 larksuite.com 域名族也被接受。
func TestParseURLLarksuiteHost(t *testing.T) {
	root, err := ParseURL("https://tenant.larksuite.com/wiki/wikcnLark")
	require.NoError(t, err)
	assert.Equal(t, sourceport.SyncRootWikiNode, root.Kind)
	assert.Equal(t, "wikcnLark", root.Token)
}

// TestParseURLBlankInput 验证纯空白输入（空格/制表符）报错。
func TestParseURLBlankInput(t *testing.T) {
	_, err := ParseURL("   ")
	require.Error(t, err)
}

// TestParseURLApexFeishuCN 验证 apex 域 feishu.cn 也被接受。
func TestParseURLApexFeishuCN(t *testing.T) {
	root, err := ParseURL("https://feishu.cn/wiki/wikcnApex")
	require.NoError(t, err)
	assert.Equal(t, sourceport.SyncRootWikiNode, root.Kind)
	assert.Equal(t, "wikcnApex", root.Token)
}

// TestParseURLStripsQueryAndFragment 验证 query/fragment 被忽略，仅取 path 段。
func TestParseURLStripsQueryAndFragment(t *testing.T) {
	root, err := ParseURL("https://tenant.feishu.cn/wiki/wikcnQ?foo=bar#/detail")
	require.NoError(t, err)
	assert.Equal(t, sourceport.SyncRootWikiNode, root.Kind)
	assert.Equal(t, "wikcnQ", root.Token)
}
