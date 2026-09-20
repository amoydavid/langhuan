package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// SourceProvider 是外部内容源连接的 provider 标识（workspace_source_connections.provider，
// spec §3.1）。连接只承载凭证与端点，不承载源级语义。
type SourceProvider string

const (
	// SourceProviderFeishu 飞书内部应用：app_id（config）+ app_secret（密文）。
	SourceProviderFeishu SourceProvider = "feishu"
	// SourceProviderFirecrawl Firecrawl 抓取服务：api_key（密文）+ 可选 endpoint 覆盖。
	SourceProviderFirecrawl SourceProvider = "firecrawl"
	// SourceProviderJina Jina Reader：api_key（密文）。
	SourceProviderJina SourceProvider = "jina"
	// SourceProviderBrowser CDP 渲染端：endpoint_url（config，非敏感）+ 可选 token（密文）。
	SourceProviderBrowser SourceProvider = "browser"
)

// Validate 拒绝未知 provider（含空值）。
func (p SourceProvider) Validate() error {
	switch p {
	case SourceProviderFeishu, SourceProviderFirecrawl, SourceProviderJina, SourceProviderBrowser:
		return nil
	default:
		return fmt.Errorf("%w: 不支持的来源 provider %q", domainerrors.ErrValidation, p)
	}
}

// RequiresAppID 报告该 provider 的连接是否必须提供 app_id。
// 仅飞书内部应用需要 app_id；firecrawl/jina 用 api_key、browser 用 endpoint_url。
func (p SourceProvider) RequiresAppID() bool {
	return p == SourceProviderFeishu
}

// RequiresCredentials 报告该 provider 的连接是否必须提供凭证密文。
// browser 允许空密文：CDP endpoint 属非敏感 config，token 是可选凭证。
func (p SourceProvider) RequiresCredentials() bool {
	switch p {
	case SourceProviderFeishu, SourceProviderFirecrawl, SourceProviderJina:
		return true
	default:
		return false
	}
}
