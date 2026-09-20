package value

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// 跟踪参数剥离名单（spec §3.3 的保守名单）。
// utm_ 前缀命中一切 utm_ 开头的键（utm_source/utm_medium/utm_campaign/utm_term/utm_content 等），
// 其余为精确键名匹配。注意：ref 在部分站点也用作业务参数，这里按 spec 裁决保守纳入剥离，
// 以保证同一页面带/不带跟踪参数时归一到同一 Document 身份。
const (
	trackingParamUTMPrefix = "utm_"
	trackingParamFBCLID    = "fbclid"
	trackingParamGCLID     = "gclid"
	trackingParamRef       = "ref"
)

// NormalizeWebSourceURI returns the stable, network-free Web Document identity.
//
// 规范化规则：scheme/host 小写、去默认端口、去 fragment、空 path 补 "/"，
// 并从 query 中剥离跟踪参数（utm_* / fbclid / gclid / ref）。
// 剥离只按 "&" 分段过滤原始 query：剩余参数保持原有顺序与 percent-encoding，
// 不经过解码/重编码，也不排序。
func NormalizeWebSourceURI(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: Web URL 无效: %v", domainerrors.ErrValidation, err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("%w: Web URL 必须是无凭证的绝对 HTTP(S) URL", domainerrors.ErrValidation)
	}

	hostname := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		u.Host = "[" + hostname + "]"
	} else {
		u.Host = hostname
	}
	if u.Path == "" {
		u.Path = "/"
	}
	u.Fragment = ""
	u.RawQuery = stripTrackingQueryParams(u.RawQuery)
	return u.String(), nil
}

// isTrackingQueryKey 报告 query 键名是否命中跟踪参数名单。
// HTTP query 键本身大小写敏感，但跟踪参数实践中大小写混用（UTM_Source 等），
// 且既有规范化输出保留键的原始大小写，因此匹配做大小写不敏感；
// 对键名不做 percent 解码（utm%5Fsource 这类编码变体极罕见，保守不处理）。
func isTrackingQueryKey(key string) bool {
	if len(key) >= len(trackingParamUTMPrefix) &&
		strings.EqualFold(key[:len(trackingParamUTMPrefix)], trackingParamUTMPrefix) {
		return true
	}
	return strings.EqualFold(key, trackingParamFBCLID) ||
		strings.EqualFold(key, trackingParamGCLID) ||
		strings.EqualFold(key, trackingParamRef)
}

// stripTrackingQueryParams 从原始 query 串中移除跟踪参数并原样重组剩余分段，
// 保持剩余参数的顺序与编码不变。仅按 "&" 分段（与 net/url 自身的 query 语义一致），
// 键为分段内第一个 "=" 之前的部分；无 "=" 的裸键也参与匹配。
func stripTrackingQueryParams(rawQuery string) string {
	if rawQuery == "" {
		return rawQuery
	}
	parts := strings.Split(rawQuery, "&")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		key := part
		if i := strings.IndexByte(part, '='); i >= 0 {
			key = part[:i]
		}
		if isTrackingQueryKey(key) {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "&")
}
