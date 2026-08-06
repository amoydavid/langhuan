package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

type KnowledgeBase struct {
	ID                      uuid.UUID
	WorkspaceID             uuid.UUID
	EmbeddingModelID        uuid.UUID
	Name                    string
	Description             string
	ChunkingConfig          value.ChunkingConfig
	Metadata                map[string]any
	ContentVersion          int64
	ActiveIndexGenerationID *uuid.UUID
	FileTreeRootID          uuid.UUID
	SourceType              value.KnowledgeBaseSourceType
	SourceConfig            map[string]any
	SourceConnectionID      *uuid.UUID
	CreatedAt               time.Time
	UpdatedAt               time.Time
	DeletedAt               *time.Time
}

// ResolvedKnowledgeBase carries a KnowledgeBase and its configured model summary.
type ResolvedKnowledgeBase struct {
	KnowledgeBase   *KnowledgeBase
	EmbeddingModel  *ResolvedModel
	RetrievalConfig map[string]any
}

// NewKnowledgeBase creates a KnowledgeBase with an explicit Embedding model reference.
func NewKnowledgeBase(workspaceID uuid.UUID, name, description string, embeddingModelID uuid.UUID, chunking *value.ChunkingConfig, metadata map[string]any) (*KnowledgeBase, error) {
	if workspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: 知识库名称不能为空", domainerrors.ErrValidation)
	}
	if embeddingModelID == uuid.Nil {
		return nil, fmt.Errorf("%w: embedding_model_id 不能为空", domainerrors.ErrValidation)
	}

	cfg := value.DefaultChunkingConfig()
	if chunking != nil {
		cfg = chunking.Normalize()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	now := time.Now().UTC()
	return &KnowledgeBase{
		ID: id.New(), WorkspaceID: workspaceID, EmbeddingModelID: embeddingModelID,
		Name: name, Description: description, ChunkingConfig: cfg,
		Metadata: metadata, SourceType: value.SourceTypeUpload, SourceConfig: map[string]any{},
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// NewKnowledgeBaseWithSource 创建一个带外部内容来源的知识库。
// 飞书来源（feishu_drive / feishu_wiki）必须绑定 sourceConnectionID，并提供 sourceConfig。
func NewKnowledgeBaseWithSource(
	workspaceID uuid.UUID,
	name, description string,
	embeddingModelID uuid.UUID,
	chunking *value.ChunkingConfig,
	metadata map[string]any,
	sourceType value.KnowledgeBaseSourceType,
	sourceConfig map[string]any,
	sourceConnectionID *uuid.UUID,
) (*KnowledgeBase, error) {
	kb, err := NewKnowledgeBase(workspaceID, name, description, embeddingModelID, chunking, metadata)
	if err != nil {
		return nil, err
	}
	if !sourceType.IsValid() {
		return nil, fmt.Errorf("%w: 未知的来源类型", domainerrors.ErrValidation)
	}
	if sourceType.IsFeishu() {
		if sourceConnectionID == nil || *sourceConnectionID == uuid.Nil {
			return nil, fmt.Errorf("%w: 飞书来源必须绑定 source_connection_id", domainerrors.ErrValidation)
		}
		if sourceConfig == nil {
			sourceConfig = map[string]any{}
		}
		if _, ok := sourceConfig["root_token"].(string); ok {
			// root_token 在 service 层进一步校验非空，这里只保证 map 存在。
		}
	}
	kb.SourceType = sourceType
	if sourceConfig == nil {
		sourceConfig = map[string]any{}
	}
	kb.SourceConfig = sourceConfig
	kb.SourceConnectionID = sourceConnectionID
	return kb, nil
}
