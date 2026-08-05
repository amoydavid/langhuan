package rerank

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// ProviderErrorKind 是不会携带供应商响应体的稳定错误分类。
type ProviderErrorKind string

const (
	ProviderErrorAuthentication  ProviderErrorKind = "authentication_failed"
	ProviderErrorTimeout         ProviderErrorKind = "request_timeout"
	ProviderErrorRateLimited     ProviderErrorKind = "rate_limited"
	ProviderErrorRejected        ProviderErrorKind = "provider_rejected"
	ProviderErrorUnreachable     ProviderErrorKind = "endpoint_unreachable"
	ProviderErrorInvalidResponse ProviderErrorKind = "invalid_rerank_response"
	ProviderErrorInputTooLarge   ProviderErrorKind = "input_too_large"
)

// ProviderError 只暴露 provider 与稳定分类，绝不保存原始 error/body。
type ProviderError struct {
	Provider string
	Kind     ProviderErrorKind
}

type httpStatusCoder interface {
	HTTPStatusCode() int
}

type statusCoder interface {
	StatusCode() int
}

// SanitizeProviderError drops upstream text and returns only a stable error class.
func SanitizeProviderError(provider string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewProviderError(provider, ProviderErrorTimeout)
	}
	if errors.Is(err, context.Canceled) {
		// context cancel 属于调用方主动取消，不在此处清洗为 Provider 错误。
		return err
	}
	var withHTTPStatus httpStatusCoder
	if errors.As(err, &withHTTPStatus) {
		return NewProviderError(provider, ProviderErrorKindForHTTPStatus(withHTTPStatus.HTTPStatusCode()))
	}
	var withStatus statusCoder
	if errors.As(err, &withStatus) {
		return NewProviderError(provider, ProviderErrorKindForHTTPStatus(withStatus.StatusCode()))
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		if networkError.Timeout() {
			return NewProviderError(provider, ProviderErrorTimeout)
		}
		return NewProviderError(provider, ProviderErrorUnreachable)
	}
	return NewProviderError(provider, ProviderErrorRejected)
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("Provider %s 重排请求失败: %s", e.Provider, e.Kind)
}

func (e *ProviderError) Is(target error) bool {
	return errors.Is(providerErrorSentinel(e.Kind), target)
}

// NewProviderError 创建不包含原始响应内容的 ProviderError。
func NewProviderError(provider string, kind ProviderErrorKind) error {
	return &ProviderError{Provider: provider, Kind: kind}
}

// ProviderErrorKindForHTTPStatus 把上游 HTTP 状态转换为稳定分类。
func ProviderErrorKindForHTTPStatus(status int) ProviderErrorKind {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ProviderErrorAuthentication
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return ProviderErrorTimeout
	case http.StatusTooManyRequests:
		return ProviderErrorRateLimited
	case http.StatusRequestEntityTooLarge:
		return ProviderErrorInputTooLarge
	default:
		return ProviderErrorRejected
	}
}

func providerErrorSentinel(kind ProviderErrorKind) error {
	switch kind {
	case ProviderErrorAuthentication:
		return domainerrors.ErrAuthenticationFailed
	case ProviderErrorTimeout:
		return domainerrors.ErrRequestTimeout
	case ProviderErrorRateLimited:
		return domainerrors.ErrRerankRateLimited
	case ProviderErrorUnreachable:
		return domainerrors.ErrRerankUnavailable
	case ProviderErrorInvalidResponse:
		return domainerrors.ErrInvalidRerankResponse
	case ProviderErrorInputTooLarge:
		return domainerrors.ErrRerankInputTooLarge
	default:
		return domainerrors.ErrRerankUnavailable
	}
}
