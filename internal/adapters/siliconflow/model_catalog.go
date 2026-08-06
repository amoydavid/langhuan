package siliconflow

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

const maxCatalogResponseBytes = 2 << 20
const maxCatalogItems = 200

type modelCatalog struct{}

type modelCatalogResponse struct {
	Data   []modelCatalogRecord `json:"data"`
	Models []modelCatalogRecord `json:"models"`
}

type modelCatalogRecord struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnedBy     string `json:"owned_by"`
}

func (c *modelCatalog) ListModels(ctx context.Context, input modelcatalogport.Input) ([]modelcatalogport.Item, error) {
	config, credentials, err := decodeNormalized(input.Config, input.CredentialsJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: SiliconFlow 目录配置无效", domainerrors.ErrCatalogUnavailable)
	}
	httpClient, err := providerutil.NewHTTPClient(input.Scope, config.BaseURL, timeDurationSeconds(config.TimeoutSeconds), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: SiliconFlow 目录客户端不可用", domainerrors.ErrCatalogUnavailable)
	}
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/models"
	} else {
		baseURL += "/v1/models"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: SiliconFlow 目录请求无效", domainerrors.ErrCatalogUnavailable)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey)
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: SiliconFlow 目录请求失败", domainerrors.ErrCatalogUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: SiliconFlow 目录返回 HTTP %d", domainerrors.ErrCatalogUnavailable, response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogResponseBytes+1))
	if err != nil || len(raw) > maxCatalogResponseBytes {
		return nil, fmt.Errorf("%w: SiliconFlow 目录响应过大或读取失败", domainerrors.ErrCatalogUnavailable)
	}
	records, err := decodeModelCatalogRecords(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: SiliconFlow 目录响应格式无效", domainerrors.ErrCatalogUnavailable)
	}
	items := make([]modelcatalogport.Item, 0, minInt(len(records), maxCatalogItems))
	query := strings.ToLower(strings.TrimSpace(input.Query))
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
		modelType, dimensions := inferModelMetadata(id, input.ModelType)
		if input.ModelType != nil && modelType != nil && *modelType != *input.ModelType {
			continue
		}
		parameters := defaultModelParameters(modelType)
		items = append(items, modelcatalogport.Item{
			ID: id, DisplayName: displayName, Description: description,
			Type: modelType, Dimensions: dimensions, Parameters: parameters, Available: true,
		})
		if len(items) >= maxCatalogItems {
			break
		}
	}
	return items, nil
}

func decodeModelCatalogRecords(raw []byte) ([]modelCatalogRecord, error) {
	var envelope modelCatalogResponse
	if err := json.Unmarshal(raw, &envelope); err == nil {
		if envelope.Data != nil {
			return envelope.Data, nil
		}
		if envelope.Models != nil {
			return envelope.Models, nil
		}
	}
	var records []modelCatalogRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func inferModelMetadata(id string, requested *value.ModelType) (*value.ModelType, *int) {
	lower := strings.ToLower(id)
	modelType := requested
	if strings.Contains(lower, "rerank") || strings.Contains(lower, "cross-encoder") {
		inferred := value.ModelTypeRerank
		modelType = &inferred
	} else if strings.Contains(lower, "embed") || strings.Contains(lower, "bge-m3") || strings.Contains(lower, "bge-large") {
		inferred := value.ModelTypeEmbedding
		modelType = &inferred
	}
	if modelType == nil || *modelType != value.ModelTypeEmbedding {
		return modelType, nil
	}
	if strings.Contains(lower, "bge-m3") || strings.Contains(lower, "bge-large") {
		dimension := value.DefaultEmbeddingDimension
		return modelType, &dimension
	}
	return modelType, nil
}

func defaultModelParameters(modelType *value.ModelType) map[string]any {
	if modelType == nil {
		return map[string]any{}
	}
	if *modelType == value.ModelTypeEmbedding {
		return map[string]any{"batch_size": 32}
	}
	if *modelType == value.ModelTypeRerank {
		return map[string]any{"max_documents": 100, "max_query_chars": 4096, "max_document_chars": 8192}
	}
	return map[string]any{}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// timeDurationSeconds keeps the adapter independent from SiliconFlow's
// internal default constants while retaining the normalized timeout contract.
func timeDurationSeconds(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}
