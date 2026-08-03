package db

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// workspaceAPIKeyToRow 把领域模型转成持久化 Row。
//
// ciphertext 不参与 codec：它由 repository 在创建时单独写入，避免 codec
// 隐含持久化细节。KnowledgeBaseIDs 由绑定表单独存储，不落在 Row 上。
func workspaceAPIKeyToRow(key *model.WorkspaceAPIKey) *WorkspaceAPIKeyRow {
	if key == nil {
		return nil
	}
	return &WorkspaceAPIKeyRow{
		ID:          key.ID,
		WorkspaceID: key.WorkspaceID,
		Name:        key.Name,
		TokenHash:   key.TokenHash,
		// TokenSecretCiphertext 由 repository 单独处理。
		TokenPrefix: key.TokenPrefix,
		Scopes:      apiScopesToStrings(key.Scopes),
		ExpiresAt:   key.ExpiresAt,
		LastUsedAt:  key.LastUsedAt,
		RevokedAt:   key.RevokedAt,
		CreatedBy:   key.CreatedBy,
		RevokedBy:   key.RevokedBy,
		CreatedAt:   key.CreatedAt,
		UpdatedAt:   key.UpdatedAt,
	}
}

// workspaceAPIKeyFromRow 把 Row 转回领域模型。KnowledgeBaseIDs 需由调用方
// 从绑定表另行加载并填充。
func workspaceAPIKeyFromRow(row *WorkspaceAPIKeyRow) *model.WorkspaceAPIKey {
	if row == nil {
		return nil
	}
	return &model.WorkspaceAPIKey{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		TokenHash:   row.TokenHash,
		TokenPrefix: row.TokenPrefix,
		Scopes:      stringsToAPIScopes(row.Scopes),
		ExpiresAt:   row.ExpiresAt,
		LastUsedAt:  row.LastUsedAt,
		RevokedAt:   row.RevokedAt,
		CreatedBy:   row.CreatedBy,
		RevokedBy:   row.RevokedBy,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

// validateWorkspaceAPIKeyRecord 在持久化前校验领域事实，确保 codec 不接收
// 空 UUID、非法 scope 或空绑定。
func validateWorkspaceAPIKeyRecord(record service.WorkspaceAPIKeyCreateRecord) error {
	if record.Key == nil {
		return fmt.Errorf("%w: API Key 不能为空", domainerrors.ErrValidation)
	}
	if record.Key.ID == uuid.Nil || record.Key.WorkspaceID == uuid.Nil {
		return fmt.Errorf("%w: API Key ID/WorkspaceID 不能为空", domainerrors.ErrValidation)
	}
	if len(record.SecretCiphertext) == 0 {
		return fmt.Errorf("%w: API Key 密文不能为空", domainerrors.ErrValidation)
	}
	if len(record.KnowledgeBaseIDs) == 0 {
		return fmt.Errorf("%w: API Key 至少绑定一个知识库", domainerrors.ErrValidation)
	}
	for _, scope := range record.Key.Scopes {
		if !value.IsValidAPIScope(scope) {
			return fmt.Errorf("%w: 非法 scope %q", domainerrors.ErrValidation, scope)
		}
	}
	return nil
}

func apiScopesToStrings(scopes []value.APIScope) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, string(scope))
	}
	return out
}

func stringsToAPIScopes(scopes []string) []value.APIScope {
	out := make([]value.APIScope, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, value.APIScope(scope))
	}
	return out
}
