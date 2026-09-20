package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	id "github.com/dajee/langhuan/internal/domain/id"
	"github.com/dajee/langhuan/internal/domain/value"
)

// KbSource 表示知识库下的一个内容源（kb_sources 行，spec §3.2）：
// 一个 KB 可挂多个源，源抓到的页面与上传文件混居同一 KB，
// 由 Document.KbSourceID 记录溯源。
type KbSource struct {
	ID              uuid.UUID
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	Kind            value.KbSourceKind
	Name            string
	Status          value.KbSourceStatus
	// Config 是源级配置（kb_sources.config jsonb 解码后的原始 map）。
	// 强类型解析（value.ParseWebSourceConfig）与天花板校验由 application 层完成——
	// 天花板随部署配置注入，domain 不读配置；与 KnowledgeBase.SourceConfig 惯例一致。
	Config map[string]any
	// 同步运行态：由系统维护（§4.1 禁止经 config 写入），语义与飞书 KB 上的
	// 同名状态一致（cursor watermark / 下次调度时间 / force latch / 上次结果摘要）。
	SyncCursor         map[string]any
	NextSyncAt         *time.Time
	SyncRequestedForce bool
	SyncLastResult     map[string]any
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
}

// NewKbSourceInput 是创建 KbSource 的输入。
type NewKbSourceInput struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	// Kind 必填；v1.3.0 仅 value.KbSourceKindWeb。
	Kind value.KbSourceKind
	Name string
	// Status 为空时取 active（与建表默认一致）。
	Status value.KbSourceStatus
	// Config 为源级配置原始 map；nil 视为空 map，不做语义校验（见 KbSource.Config）。
	Config map[string]any
}

// NewKbSource 创建一个内容源；新源的运行态列取建表默认值
// （cursor/last_result 为空 map、未设 next_sync_at、force latch 关闭）。
func NewKbSource(input NewKbSourceInput) (*KbSource, error) {
	if input.WorkspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	if input.KnowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: kb_id 不能为空", domainerrors.ErrValidation)
	}
	if err := input.Kind.Validate(); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: 源名称不能为空", domainerrors.ErrValidation)
	}
	status := input.Status
	if status == "" {
		status = value.KbSourceStatusActive
	}
	if err := status.Validate(); err != nil {
		return nil, err
	}
	config := input.Config
	if config == nil {
		config = map[string]any{}
	}
	now := time.Now().UTC()
	return &KbSource{
		ID:              id.New(),
		WorkspaceID:     input.WorkspaceID,
		KnowledgeBaseID: input.KnowledgeBaseID,
		Kind:            input.Kind,
		Name:            name,
		Status:          status,
		Config:          config,
		SyncCursor:      map[string]any{},
		SyncLastResult:  map[string]any{},
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}
