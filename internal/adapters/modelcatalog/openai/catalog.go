// Package openai implements OpenAI-compatible Provider model discovery.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dajee/langhuan/internal/adapters/providerutil"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	modelcatalogport "github.com/dajee/langhuan/internal/ports/modelcatalog"
)

const maxResponseBytes = 2 << 20
const maxItems = 200

// Catalog lists models from an OpenAI-compatible /models endpoint.
type Catalog struct{}

// NewCatalog creates a stateless OpenAI-compatible catalog adapter.
func NewCatalog() *Catalog { return &Catalog{} }

type credentials struct {
	APIKey        string            `json:"api_key"`
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
}

type responseEnvelope struct {
	Data   []record `json:"data"`
	Models []record `json:"models"`
}

type record struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnedBy     string `json:"owned_by"`
}

func (c *Catalog) ListModels(ctx context.Context, input modelcatalogport.Input) ([]modelcatalogport.Item, error) {
	baseURL, timeout, mode, err := readConfig(input.Config)
	if err != nil || mode == "azure" {
		return nil, domainerrors.ErrCatalogUnavailable
	}
	var credential credentials
	if err := providerutil.DecodeStrict(input.CredentialsJSON, &credential, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, domainerrors.ErrCatalogUnavailable
	}
	httpClient, err := providerutil.NewHTTPClient(input.Scope, baseURL, timeout, credential.CustomHeaders)
	if err != nil {
		return nil, domainerrors.ErrCatalogUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsEndpoint(baseURL), nil)
	if err != nil {
		return nil, domainerrors.ErrCatalogUnavailable
	}
	request.Header.Set("Accept", "application/json")
	if strings.TrimSpace(credential.APIKey) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(credential.APIKey))
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: 模型目录请求失败", domainerrors.ErrCatalogUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: 模型目录返回 HTTP %d", domainerrors.ErrCatalogUnavailable, response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("%w: 模型目录响应过大或读取失败", domainerrors.ErrCatalogUnavailable)
	}
	records, err := decodeRecords(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: 模型目录响应格式无效", domainerrors.ErrCatalogUnavailable)
	}
	query := strings.ToLower(strings.TrimSpace(input.Query))
	items := make([]modelcatalogport.Item, 0, min(len(records), maxItems))
	for _, record := range records {
		id := strings.TrimSpace(record.ID)
		if id == "" {
			id = strings.TrimSpace(record.Name)
		}
		if id == "" {
			continue
		}
		displayName := strings.TrimSpace(record.Name)
		if displayName == "" {
			displayName = id
		}
		description := strings.TrimSpace(record.Description)
		if description == "" {
			description = strings.TrimSpace(record.OwnedBy)
		}
		if query != "" && !strings.Contains(strings.ToLower(id+" "+displayName+" "+description), query) {
			continue
		}
		parameters := map[string]any{}
		if input.ModelType != nil && *input.ModelType == value.ModelTypeEmbedding {
			parameters["batch_size"] = 32
		}
		items = append(items, modelcatalogport.Item{
			ID: id, DisplayName: displayName, Description: description,
			Type: cloneType(input.ModelType), Parameters: parameters, Available: true,
		})
		if len(items) >= maxItems {
			break
		}
	}
	return items, nil
}

func readConfig(config map[string]any) (string, time.Duration, string, error) {
	baseURL, _ := config["base_url"].(string)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	timeoutSeconds := 60
	switch value := config["timeout_seconds"].(type) {
	case int:
		timeoutSeconds = value
	case float64:
		timeoutSeconds = int(value)
	}
	if err := providerutil.ValidateTimeout(timeoutSeconds); err != nil {
		return "", 0, "", err
	}
	mode, _ := config["mode"].(string)
	return baseURL, time.Duration(timeoutSeconds) * time.Second, strings.ToLower(strings.TrimSpace(mode)), nil
}

func modelsEndpoint(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/models") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/models"
	}
	return baseURL + "/v1/models"
}

func decodeRecords(raw []byte) ([]record, error) {
	var envelope responseEnvelope
	if err := json.Unmarshal(raw, &envelope); err == nil {
		if envelope.Data != nil {
			return envelope.Data, nil
		}
		if envelope.Models != nil {
			return envelope.Models, nil
		}
	}
	var records []record
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func cloneType(input *value.ModelType) *value.ModelType {
	if input == nil {
		return nil
	}
	cloned := *input
	return &cloned
}
