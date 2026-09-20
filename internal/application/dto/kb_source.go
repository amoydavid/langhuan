package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

// KbSource 是知识库内容源的 API 表示（kb_sources 行，v1.3.0 spec §3.2/§9.2）。
// Config 返回源级配置全量：web 源 config 不含凭证（连接凭证加密存于
// source_connections，绝不进入本 DTO），因此可整体返回供管理台回显；
// 同步运行态字段（cursor/next_sync_at/last_result）由系统维护，供展示调度状态。
type KbSource struct {
	ID                 uuid.UUID      `json:"id"`
	WorkspaceID        uuid.UUID      `json:"workspace_id"`
	KnowledgeBaseID    uuid.UUID      `json:"knowledge_base_id"`
	Kind               string         `json:"kind"`
	Name               string         `json:"name"`
	Status             string         `json:"status"`
	Config             map[string]any `json:"config"`
	SyncCursor         map[string]any `json:"sync_cursor"`
	NextSyncAt         *time.Time     `json:"next_sync_at"`
	SyncRequestedForce bool           `json:"sync_requested_force"`
	SyncLastResult     map[string]any `json:"sync_last_result"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// KbSourceFromModel 把领域模型转成 API DTO；nil 安全。
func KbSourceFromModel(source *model.KbSource) *KbSource {
	if source == nil {
		return nil
	}
	config := source.Config
	if config == nil {
		config = map[string]any{}
	}
	syncCursor := source.SyncCursor
	if syncCursor == nil {
		syncCursor = map[string]any{}
	}
	syncLastResult := source.SyncLastResult
	if syncLastResult == nil {
		syncLastResult = map[string]any{}
	}
	return &KbSource{
		ID:                 source.ID,
		WorkspaceID:        source.WorkspaceID,
		KnowledgeBaseID:    source.KnowledgeBaseID,
		Kind:               string(source.Kind),
		Name:               source.Name,
		Status:             string(source.Status),
		Config:             config,
		SyncCursor:         syncCursor,
		NextSyncAt:         source.NextSyncAt,
		SyncRequestedForce: source.SyncRequestedForce,
		SyncLastResult:     syncLastResult,
		CreatedAt:          source.CreatedAt,
		UpdatedAt:          source.UpdatedAt,
	}
}
