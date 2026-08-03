package db

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func modelProviderToRow(provider *model.ModelProvider) (*ModelProviderRow, error) {
	if provider == nil {
		return nil, fmt.Errorf("model provider 不能为空")
	}
	if !provider.Scope.IsValid() || !provider.Status.IsValid() {
		return nil, fmt.Errorf("model provider scope/status 无效")
	}
	return &ModelProviderRow{
		ID:                    provider.ID,
		Scope:                 string(provider.Scope),
		WorkspaceID:           cloneUUIDPointer(provider.WorkspaceID),
		Name:                  provider.Name,
		DisplayName:           provider.DisplayName,
		Description:           provider.Description,
		Provider:              provider.Provider,
		Config:                cloneJSONMap(provider.Config),
		CredentialsCiphertext: append([]byte(nil), provider.CredentialsCiphertext...),
		Status:                string(provider.Status),
		CreatedBy:             cloneUUIDPointer(provider.CreatedBy),
		CreatedAt:             provider.CreatedAt,
		UpdatedAt:             provider.UpdatedAt,
	}, nil
}

func modelProviderFromRow(row *ModelProviderRow) (*model.ModelProvider, error) {
	if row == nil {
		return nil, fmt.Errorf("model provider row 不能为空")
	}
	scope := value.ModelScope(row.Scope)
	status := value.ModelStatus(row.Status)
	if !scope.IsValid() {
		return nil, fmt.Errorf("读取 ModelProvider 失败: scope %q 无效", row.Scope)
	}
	if !status.IsValid() {
		return nil, fmt.Errorf("读取 ModelProvider 失败: status %q 无效", row.Status)
	}
	return &model.ModelProvider{
		ID:                    row.ID,
		Scope:                 scope,
		WorkspaceID:           cloneUUIDPointer(row.WorkspaceID),
		Name:                  row.Name,
		DisplayName:           row.DisplayName,
		Description:           row.Description,
		Provider:              row.Provider,
		Config:                map[string]any(cloneJSONMap(map[string]any(row.Config))),
		CredentialsCiphertext: append([]byte(nil), row.CredentialsCiphertext...),
		Status:                status,
		CreatedBy:             cloneUUIDPointer(row.CreatedBy),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}, nil
}

func cloneJSONMap(input map[string]any) JSONMap {
	if input == nil {
		return JSONMap{}
	}
	cloned := make(JSONMap, len(input))
	for key, item := range input {
		cloned[key] = item
	}
	return cloned
}

func cloneUUIDPointer(input *uuid.UUID) *uuid.UUID {
	if input == nil {
		return nil
	}
	cloned := *input
	return &cloned
}
