package compatible

import (
	"fmt"
	"net/textproto"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// reservedHeaderNames 禁止 custom_headers 覆盖的协议与安全相关 header。
var reservedHeaderNames = map[string]struct{}{
	"authorization":       {},
	"host":                {},
	"content-length":      {},
	"content-type":        {},
	"accept-encoding":     {},
	"cookie":              {},
	"set-cookie":          {},
	"connection":          {},
	"proxy-authorization": {},
	"proxy-authenticate":  {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"te":                  {},
	"trailer":             {},
}

// validateCustomHeaders 校验 custom_headers 数量、长度、保留 header 与 CR/LF 注入。
func validateCustomHeaders(headers map[string]string) error {
	if len(headers) > maxCustomHeaders {
		return fmt.Errorf("%w: custom_headers 数量超过 %d", domainerrors.ErrInvalidProviderConfig, maxCustomHeaders)
	}
	for name, value := range headers {
		if name == "" {
			return fmt.Errorf("%w: custom_headers 名称不能为空", domainerrors.ErrInvalidProviderConfig)
		}
		if len(name) > maxCustomHeaderNameLen {
			return fmt.Errorf("%w: custom_headers 名称过长", domainerrors.ErrInvalidProviderConfig)
		}
		if len(value) > maxCustomHeaderValueLen {
			return fmt.Errorf("%w: custom_headers 值过长", domainerrors.ErrInvalidProviderConfig)
		}
		if strings.ContainsAny(name, "\r\n") || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%w: custom_headers 不能包含换行", domainerrors.ErrInvalidProviderConfig)
		}
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if canonical == "" {
			return fmt.Errorf("%w: custom_headers 名称无效: %q", domainerrors.ErrInvalidProviderConfig, name)
		}
		if _, reserved := reservedHeaderNames[strings.ToLower(name)]; reserved {
			return fmt.Errorf("%w: 不允许设置 custom_headers: %s", domainerrors.ErrInvalidProviderConfig, canonical)
		}
	}
	return nil
}
