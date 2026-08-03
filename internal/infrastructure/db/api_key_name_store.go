package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
)

// APIKeyNameStoreDB 是 service.APIKeyNameStore 的 GORM 实现，解析 API Key
// 摘要所需的可读知识库名称与创建者昵称。
type APIKeyNameStoreDB struct {
	db *gorm.DB
}

// NewAPIKeyNameStoreDB 构造 API Key 名称解析适配器。
func NewAPIKeyNameStoreDB(db *gorm.DB) *APIKeyNameStoreDB {
	return &APIKeyNameStoreDB{db: db}
}

// KnowledgeBaseNames 返回给定 workspace 下指定知识库 ID 的 (id, name) 映射。
// 不存在的 ID 不会出现在结果中。
func (s *APIKeyNameStoreDB) KnowledgeBaseNames(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	type kbName struct {
		ID   uuid.UUID
		Name string
	}
	var rows []kbName
	if err := s.db.WithContext(ctx).
		Table("knowledge_bases").
		Select("id, name").
		Where("workspace_id = ? AND id IN ?", workspaceID, ids).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取知识库名称失败: %w", err)
	}
	for _, row := range rows {
		out[row.ID] = row.Name
	}
	return out, nil
}

// UserNickname 返回给定用户 ID 的昵称；不存在返回空串。
func (s *APIKeyNameStoreDB) UserNickname(ctx context.Context, userID uuid.UUID) (string, error) {
	if userID == uuid.Nil {
		return "", nil
	}
	var nickname string
	err := s.db.WithContext(ctx).
		Table("users").
		Select("nickname").
		Where("id = ?", userID).
		Limit(1).
		Scan(&nickname).Error
	if err != nil {
		return "", fmt.Errorf("读取用户昵称失败: %w", err)
	}
	return nickname, nil
}

var _ service.APIKeyNameStore = (*APIKeyNameStoreDB)(nil)
