// Package providerutil contains the shared serialization and HTTP boundary
// used by both the Embedding and Rerank Provider factories.
//
// 它把跨能力复用的 strict JSON、typed map、Provider 超时/批量/维度校验
// 和 SSRF-safe HTTP client 构造集中在一处，避免每个 adapter 重新实现或
// 继续耦合在 embedding 内部包里。
package providerutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dajee/langhuan/internal/adapters/httpclient"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// DecodeStrict decodes exactly one JSON object and rejects unknown fields.
func DecodeStrict(raw json.RawMessage, target any, class error) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		raw = json.RawMessage(`{}`)
	} else if trimmed[0] != '{' {
		return fmt.Errorf("%w: JSON 必须是对象", class)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", class, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: JSON 只能包含一个对象", class)
	}
	return nil
}

// DecodeMap strictly decodes a normalized JSONB map into a typed struct.
func DecodeMap(source map[string]any, target any) error {
	raw, err := json.Marshal(source)
	if err != nil {
		return fmt.Errorf("%w: 编码持久化配置失败", domainerrors.ErrInvalidProviderConfig)
	}
	return DecodeStrict(raw, target, domainerrors.ErrInvalidProviderConfig)
}

// ToMap encodes a typed config as a detached JSON-compatible map.
func ToMap(source any) (map[string]any, error) {
	raw, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("%w: 编码 Provider 配置失败", domainerrors.ErrInvalidProviderConfig)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%w: 编码 Provider 配置失败", domainerrors.ErrInvalidProviderConfig)
	}
	return result, nil
}

// ToJSON encodes typed credentials for encryption.
func ToJSON(source any) ([]byte, error) {
	raw, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("%w: 编码 Provider 凭证失败", domainerrors.ErrInvalidProviderConfig)
	}
	return raw, nil
}

// ValidateTimeout applies the common Provider timeout contract.
func ValidateTimeout(seconds int) error {
	if seconds < 1 || seconds > 600 {
		return fmt.Errorf("%w: timeout_seconds 必须在 1 到 600 之间", domainerrors.ErrInvalidProviderConfig)
	}
	return nil
}

// ValidateBatchSize applies the common Provider batching contract.
func ValidateBatchSize(size int) error {
	if size < 1 || size > 200 {
		return fmt.Errorf("%w: batch_size 必须在 1 到 200 之间", domainerrors.ErrInvalidProviderConfig)
	}
	return nil
}

// ValidateEmbeddingModel applies common model identity and indexed-dimension rules.
func ValidateEmbeddingModel(name string, dimensions int) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: model_name 不能为空", domainerrors.ErrInvalidProviderConfig)
	}
	if !value.IsSupportedEmbeddingDimension(dimensions) {
		return fmt.Errorf("%w: %d", domainerrors.ErrUnsupportedEmbeddingDimension, dimensions)
	}
	return nil
}

// NewHTTPClient selects the public Workspace policy only for custom endpoints.
// Official SDK endpoints and all platform-managed endpoints remain trusted.
func NewHTTPClient(scope value.ModelScope, baseURL string, timeout time.Duration, headers map[string]string) (*http.Client, error) {
	if scope == value.ModelScopeWorkspace && strings.TrimSpace(baseURL) != "" {
		client, err := httpclient.NewPublicHTTPSClient(httpclient.PublicClientConfig{
			BaseURL: baseURL,
			Timeout: timeout,
			Headers: headers,
		})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", domainerrors.ErrInvalidProviderConfig, err)
		}
		return client, nil
	}
	client, err := httpclient.NewTrustedClient(timeout, headers)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domainerrors.ErrInvalidProviderConfig, err)
	}
	return client, nil
}
