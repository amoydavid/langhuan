package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

var modelIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// Model 表示 Provider 下一个具体、可选择的模型实例。
type Model struct {
	ID          uuid.UUID
	ProviderID  uuid.UUID
	Name        string
	DisplayName string
	Description string
	Type        value.ModelType
	ModelName   string
	Dimensions  *int
	Parameters  map[string]any
	Status      value.ModelStatus
	CreatedBy   *uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ResolvedModel 是执行连接测试或模型选择时使用的 Model + Provider 聚合。
type ResolvedModel struct {
	Model    *Model
	Provider *ModelProvider
}

// NewModel 创建一个 active Model，并校验类型与维度组合。
func NewModel(
	providerID uuid.UUID,
	name string,
	displayName string,
	description string,
	modelType value.ModelType,
	modelName string,
	dimensions *int,
	parameters map[string]any,
	createdBy uuid.UUID,
) (*Model, error) {
	if providerID == uuid.Nil {
		return nil, fmt.Errorf("%w: provider_id 不能为空", domainerrors.ErrValidation)
	}
	if createdBy == uuid.Nil {
		return nil, fmt.Errorf("%w: created_by 不能为空", domainerrors.ErrValidation)
	}
	if !modelType.IsValid() {
		return nil, fmt.Errorf("%w: model type 无效", domainerrors.ErrValidation)
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" || utf8.RuneCountInString(modelName) > 255 {
		return nil, fmt.Errorf("%w: model_name 必须为 1 到 255 个 Unicode 字符", domainerrors.ErrValidation)
	}
	if modelType == value.ModelTypeEmbedding {
		if dimensions == nil || !value.IsSupportedEmbeddingDimension(*dimensions) {
			return nil, fmt.Errorf("%w: %w", domainerrors.ErrValidation, domainerrors.ErrUnsupportedEmbeddingDimension)
		}
	} else if dimensions != nil {
		return nil, fmt.Errorf("%w: 非 Embedding 模型不得设置 dimensions", domainerrors.ErrValidation)
	}

	normalizedName, normalizedDisplayName, normalizedDescription, err := normalizeModelText(name, displayName, description)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	actorID := createdBy
	return &Model{
		ID:          uuid.New(),
		ProviderID:  providerID,
		Name:        normalizedName,
		DisplayName: normalizedDisplayName,
		Description: normalizedDescription,
		Type:        modelType,
		ModelName:   modelName,
		Dimensions:  cloneIntPointer(dimensions),
		Parameters:  cloneMap(parameters),
		Status:      value.ModelStatusActive,
		CreatedBy:   &actorID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func normalizeModelText(name, displayName, description string) (string, string, string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !modelIdentifierPattern.MatchString(name) {
		return "", "", "", fmt.Errorf("%w: name 必须是 1 到 64 位小写 ASCII 标识", domainerrors.ErrValidation)
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = name
	}
	if utf8.RuneCountInString(displayName) > 255 {
		return "", "", "", fmt.Errorf("%w: display_name 不能超过 255 个 Unicode 字符", domainerrors.ErrValidation)
	}
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > 2000 {
		return "", "", "", fmt.Errorf("%w: description 不能超过 2000 个 Unicode 字符", domainerrors.ErrValidation)
	}
	return name, displayName, description, nil
}

func cloneUUIDPointer(input *uuid.UUID) *uuid.UUID {
	if input == nil {
		return nil
	}
	cloned := *input
	return &cloned
}

func cloneIntPointer(input *int) *int {
	if input == nil {
		return nil
	}
	cloned := *input
	return &cloned
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(input))
	for key, item := range input {
		cloned[key] = item
	}
	return cloned
}
