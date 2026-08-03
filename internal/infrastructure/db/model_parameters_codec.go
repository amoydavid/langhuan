package db

import (
	"fmt"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func modelToRow(input *model.Model) (*ModelRow, error) {
	if input == nil {
		return nil, fmt.Errorf("model 不能为空")
	}
	if !input.Type.IsValid() || !input.Status.IsValid() {
		return nil, fmt.Errorf("model type/status 无效")
	}
	return &ModelRow{
		ID:          input.ID,
		ProviderID:  input.ProviderID,
		Name:        input.Name,
		DisplayName: input.DisplayName,
		Description: input.Description,
		Type:        string(input.Type),
		ModelName:   input.ModelName,
		Dimensions:  cloneIntPointer(input.Dimensions),
		Parameters:  cloneJSONMap(input.Parameters),
		Status:      string(input.Status),
		CreatedBy:   cloneUUIDPointer(input.CreatedBy),
		CreatedAt:   input.CreatedAt,
		UpdatedAt:   input.UpdatedAt,
	}, nil
}

func modelFromRow(row *ModelRow) (*model.Model, error) {
	if row == nil {
		return nil, fmt.Errorf("model row 不能为空")
	}
	modelType := value.ModelType(row.Type)
	status := value.ModelStatus(row.Status)
	if !modelType.IsValid() {
		return nil, fmt.Errorf("读取 Model 失败: type %q 无效", row.Type)
	}
	if !status.IsValid() {
		return nil, fmt.Errorf("读取 Model 失败: status %q 无效", row.Status)
	}
	return &model.Model{
		ID:          row.ID,
		ProviderID:  row.ProviderID,
		Name:        row.Name,
		DisplayName: row.DisplayName,
		Description: row.Description,
		Type:        modelType,
		ModelName:   row.ModelName,
		Dimensions:  cloneIntPointer(row.Dimensions),
		Parameters:  map[string]any(cloneJSONMap(map[string]any(row.Parameters))),
		Status:      status,
		CreatedBy:   cloneUUIDPointer(row.CreatedBy),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func cloneIntPointer(input *int) *int {
	if input == nil {
		return nil
	}
	cloned := *input
	return &cloned
}
