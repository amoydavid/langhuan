package http

import (
	"errors"
	"log/slog"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// internalErrorMessage 是返回给客户端的 500 通用文案，绝不包含原始错误细节。
const internalErrorMessage = "服务器内部错误"

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorBody{Error: errorPayload{Code: code, Message: message}})
}

// writeInternalError 记录原始错误详情到服务端日志，并向客户端返回通用 500 文案，
// 避免把底层错误（驱动细节、栈、连接串等）泄漏到响应体。
func writeInternalError(c *gin.Context, err error) {
	slog.Error("处理请求失败",
		slog.String("path", c.Request.URL.Path),
		slog.String("method", c.Request.Method),
		slog.Any("error", err),
	)
	writeError(c, stdhttp.StatusInternalServerError, "internal_error", internalErrorMessage)
}

func statusForError(err error) int {
	if status, _, _, ok := modelErrorDetails(err); ok {
		return status
	}
	switch {
	case errors.Is(err, domainerrors.ErrNotFound):
		return stdhttp.StatusNotFound
	case errors.Is(err, domainerrors.ErrValidation):
		return stdhttp.StatusBadRequest
	case errors.Is(err, domainerrors.ErrUnauthorized):
		return stdhttp.StatusUnauthorized
	case errors.Is(err, domainerrors.ErrForbidden):
		return stdhttp.StatusForbidden
	case errors.Is(err, domainerrors.ErrConflict):
		return stdhttp.StatusConflict
	case errors.Is(err, domainerrors.ErrRateLimited):
		return stdhttp.StatusTooManyRequests
	case errors.Is(err, domainerrors.ErrUnsupportedFileType):
		return stdhttp.StatusUnsupportedMediaType
	default:
		return stdhttp.StatusInternalServerError
	}
}

// codeForStatus maps an HTTP status to the stable error `code` string used in the
// {Error:{Code,Message}} response body.
func codeForStatus(status int) string {
	switch status {
	case stdhttp.StatusBadRequest:
		return "validation_error"
	case stdhttp.StatusNotFound:
		return "not_found"
	case stdhttp.StatusUnauthorized:
		return "unauthorized"
	case stdhttp.StatusForbidden:
		return "forbidden"
	case stdhttp.StatusConflict:
		return "conflict"
	case stdhttp.StatusTooManyRequests:
		return "rate_limited"
	case stdhttp.StatusUnsupportedMediaType:
		return "unsupported_file_type"
	default:
		return "internal_error"
	}
}

func writeServiceError(c *gin.Context, err error) {
	if errors.Is(err, domainerrors.ErrCredentialDecryption) {
		slog.Error("模型凭证解密失败",
			slog.String("path", c.Request.URL.Path),
			slog.String("method", c.Request.Method),
			slog.String("error_class", "credential_decryption_failed"),
			slog.Any("error", err),
		)
		writeError(c, stdhttp.StatusInternalServerError, "internal_error", internalErrorMessage)
		return
	}
	if errors.Is(err, domainerrors.ErrAPIKeySecretUnavailable) {
		slog.Error("API Key 明文不可恢复",
			slog.String("path", c.Request.URL.Path),
			slog.String("method", c.Request.Method),
			slog.String("error_class", "api_key_secret_unavailable"),
			slog.Any("error", err),
		)
		writeError(c, stdhttp.StatusInternalServerError, "api_key_secret_unavailable", domainerrors.ErrAPIKeySecretUnavailable.Error())
		return
	}
	if status, code, message, ok := modelErrorDetails(err); ok {
		writeError(c, status, code, message)
		return
	}
	status := statusForError(err)
	if status == stdhttp.StatusInternalServerError {
		writeInternalError(c, err)
		return
	}
	if errors.Is(err, domainerrors.ErrUnsupportedFileType) {
		writeError(c, status, codeForStatus(status), domainerrors.ErrUnsupportedFileType.Error())
		return
	}
	writeError(c, status, codeForStatus(status), err.Error())
}

func modelErrorDetails(err error) (int, string, string, bool) {
	type errorMapping struct {
		err    error
		status int
		code   string
	}
	mappings := [...]errorMapping{
		{domainerrors.ErrRevisionConflict, stdhttp.StatusConflict, "revision_conflict"},
		{domainerrors.ErrGenerationBuildInProgress, stdhttp.StatusConflict, "generation_build_in_progress"},
		{domainerrors.ErrGenerationStale, stdhttp.StatusConflict, "generation_stale"},
		{domainerrors.ErrGenerationNotReady, stdhttp.StatusConflict, "generation_not_ready"},
		{domainerrors.ErrManualEditConfirmationRequired, stdhttp.StatusConflict, "manual_edit_confirmation_required"},
		{domainerrors.ErrFAQChunkImmutable, stdhttp.StatusConflict, "faq_chunk_immutable"},
		{domainerrors.ErrFileTreeNameConflict, stdhttp.StatusConflict, "file_tree_name_conflict"},
		{domainerrors.ErrFileTreeCycle, stdhttp.StatusConflict, "file_tree_cycle"},
		{domainerrors.ErrFileTreeNotEmpty, stdhttp.StatusConflict, "file_tree_not_empty"},
		{domainerrors.ErrUnsupportedProvider, stdhttp.StatusBadRequest, "unsupported_provider"},
		{domainerrors.ErrProviderScopeNotAllowed, stdhttp.StatusBadRequest, "provider_scope_not_allowed"},
		{domainerrors.ErrInvalidProviderConfig, stdhttp.StatusBadRequest, "invalid_provider_config"},
		{domainerrors.ErrCredentialsRequired, stdhttp.StatusBadRequest, "credentials_required"},
		{domainerrors.ErrUnsupportedModelType, stdhttp.StatusBadRequest, "unsupported_model_type"},
		{domainerrors.ErrUnsupportedEmbeddingDimension, stdhttp.StatusBadRequest, "unsupported_embedding_dimension"},
		{domainerrors.ErrModelNotVisible, stdhttp.StatusNotFound, "model_not_visible"},
		{domainerrors.ErrModelDisabled, stdhttp.StatusBadRequest, "model_disabled"},
		{domainerrors.ErrProviderDisabled, stdhttp.StatusBadRequest, "provider_disabled"},
		{domainerrors.ErrDimensionMismatch, stdhttp.StatusUnprocessableEntity, "dimension_mismatch"},
		{domainerrors.ErrConnectionTestFailed, stdhttp.StatusBadGateway, "connection_test_failed"},
		{domainerrors.ErrAuthenticationFailed, stdhttp.StatusUnprocessableEntity, "authentication_failed"},
		{domainerrors.ErrEndpointUnreachable, stdhttp.StatusBadGateway, "endpoint_unreachable"},
		{domainerrors.ErrRequestTimeout, stdhttp.StatusGatewayTimeout, "request_timeout"},
		{domainerrors.ErrRateLimited, stdhttp.StatusTooManyRequests, "rate_limited"},
		{domainerrors.ErrProviderRejected, stdhttp.StatusBadGateway, "provider_rejected"},
		{domainerrors.ErrCatalogUnavailable, stdhttp.StatusBadGateway, "catalog_unavailable"},
		{domainerrors.ErrInvalidEmbeddingResponse, stdhttp.StatusUnprocessableEntity, "invalid_embedding_response"},
		{domainerrors.ErrImmutableModelField, stdhttp.StatusConflict, "immutable_model_field"},
		{domainerrors.ErrModelInUse, stdhttp.StatusConflict, "model_in_use"},
		{domainerrors.ErrProviderInUse, stdhttp.StatusConflict, "provider_in_use"},
		{domainerrors.ErrRerankConfigurationConflict, stdhttp.StatusConflict, "rerank_configuration_conflict"},
		{domainerrors.ErrRerankSnapshotMismatch, stdhttp.StatusConflict, "rerank_snapshot_mismatch"},
		{domainerrors.ErrEmbeddingSnapshotMismatch, stdhttp.StatusConflict, "embedding_snapshot_mismatch"},
		{domainerrors.ErrRerankUnavailable, stdhttp.StatusServiceUnavailable, "rerank_unavailable"},
		{domainerrors.ErrRerankRateLimited, stdhttp.StatusServiceUnavailable, "rerank_rate_limited"},
		{domainerrors.ErrInvalidRerankResponse, stdhttp.StatusBadGateway, "invalid_rerank_response"},
		{domainerrors.ErrRerankInputTooLarge, stdhttp.StatusBadRequest, "rerank_input_too_large"},
		{domainerrors.ErrAPIKeyLimitReached, stdhttp.StatusConflict, "api_key_limit_reached"},
		{domainerrors.ErrAPIKeyImmutable, stdhttp.StatusConflict, "api_key_immutable"},
		{domainerrors.ErrInsufficientScope, stdhttp.StatusForbidden, "insufficient_scope"},
		{domainerrors.ErrWorkspaceLimitReached, stdhttp.StatusConflict, "workspace_limit_reached"},
		{domainerrors.ErrIdempotencyConflict, stdhttp.StatusConflict, "idempotency_conflict"},
	}
	for _, mapping := range mappings {
		if errors.Is(err, mapping.err) {
			return mapping.status, mapping.code, mapping.err.Error(), true
		}
	}
	return 0, "", "", false
}
