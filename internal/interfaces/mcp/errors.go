package mcp

import (
	"errors"
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// mcpError 是 MCP 工具返回的稳定结构化错误，同时作为 structuredContent 与
// text JSON fallback。不泄漏底层 error。
type mcpError struct {
	Error mcpErrorBody `json:"error"`
}

type mcpErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// mapDomainError 把领域错误映射为稳定 MCP 错误。未识别错误统一 internal_error，
// 不泄漏 %v 内容。
func mapDomainError(err error) mcpError {
	if err == nil {
		return mcpError{Error: mcpErrorBody{Code: "internal_error", Message: "服务器内部错误"}}
	}
	switch {
	case errors.Is(err, domainerrors.ErrNotFound):
		return mcpError{Error: mcpErrorBody{Code: "not_found", Message: "资源不存在"}}
	case errors.Is(err, domainerrors.ErrValidation):
		return mcpError{Error: mcpErrorBody{Code: "validation_error", Message: safeMessage(err)}}
	case errors.Is(err, domainerrors.ErrForbidden):
		return mcpError{Error: mcpErrorBody{Code: "forbidden", Message: "无权限"}}
	case errors.Is(err, domainerrors.ErrInsufficientScope):
		return mcpError{Error: mcpErrorBody{Code: "insufficient_scope", Message: "API Key 权限不足"}}
	case errors.Is(err, domainerrors.ErrConflict):
		return mcpError{Error: mcpErrorBody{Code: "conflict", Message: "资源冲突"}}
	case errors.Is(err, domainerrors.ErrGenerationNotReady):
		return mcpError{Error: mcpErrorBody{Code: "generation_not_ready", Message: "知识库当前没有可用索引", Retryable: true}}
	case errors.Is(err, domainerrors.ErrGenerationStale):
		return mcpError{Error: mcpErrorBody{Code: "generation_stale", Message: "索引代次已变化", Retryable: true}}
	case errors.Is(err, domainerrors.ErrGenerationBuildInProgress):
		return mcpError{Error: mcpErrorBody{Code: "generation_build_in_progress", Message: "索引正在构建", Retryable: true}}
	case errors.Is(err, domainerrors.ErrRateLimited):
		return mcpError{Error: mcpErrorBody{Code: "rate_limited", Message: "请求过于频繁", Retryable: true}}
	case errors.Is(err, domainerrors.ErrRequestTimeout), errors.Is(err, domainerrors.ErrEndpointUnreachable):
		return mcpError{Error: mcpErrorBody{Code: "provider_unavailable", Message: "供应商暂时不可用", Retryable: true}}
	case errors.Is(err, domainerrors.ErrAPIKeyLimitReached):
		return mcpError{Error: mcpErrorBody{Code: "api_key_limit_reached", Message: "活跃 API Key 数量已达上限"}}
	case errors.Is(err, domainerrors.ErrAPIKeySecretUnavailable):
		return mcpError{Error: mcpErrorBody{Code: "api_key_secret_unavailable", Message: "API Key 明文不可恢复"}}
	}
	return mcpError{Error: mcpErrorBody{Code: "internal_error", Message: "服务器内部错误"}}
}

// safeMessage 返回领域包装错误的 message，但不包含底层 %v 细节。
func safeMessage(err error) string {
	// 仅对已知安全 sentinel 返回其 message，避免泄漏驱动细节。
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

func (e mcpError) String() string {
	return fmt.Sprintf(`{"error":{"code":%q,"message":%q,"retryable":%t}}`, e.Error.Code, e.Error.Message, e.Error.Retryable)
}
