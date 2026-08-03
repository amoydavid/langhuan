package value

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// NormalizeWebSourceURI returns the stable, network-free Web Document identity.
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
	return u.String(), nil
}
