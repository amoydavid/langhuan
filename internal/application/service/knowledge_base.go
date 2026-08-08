package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// KnowledgeBaseSyncEnqueuer 把飞书知识库的首次同步入队。
// 由 *SourceSyncService 实现（EnqueueSync）；为 nil 时 KnowledgeBaseService.Create 跳过首次同步。
type KnowledgeBaseSyncEnqueuer interface {
	EnqueueSync(ctx context.Context, workspaceID, kbID uuid.UUID, options SyncOptions) (*model.Job, error)
}

// KnowledgeBaseService manages KnowledgeBases.
type KnowledgeBaseService struct {
	binder             KnowledgeBaseModelBinder
	store              KnowledgeBaseCreateStore
	updater            KnowledgeBaseBasicsUpdater
	policyUpdater      KnowledgeBaseSourcePolicyUpdater
	syncEnqueuer       KnowledgeBaseSyncEnqueuer
	syncEnqueuerLogger *slog.Logger
}

// CreateKnowledgeBaseInput contains KnowledgeBase creation fields.
type CreateKnowledgeBaseInput struct {
	WorkspaceID      uuid.UUID
	Name             string
	Description      string
	EmbeddingModelID uuid.UUID
	ChunkingConfig   *value.ChunkingConfig
	// CallerAPIKeyID 在 API Key 主体创建知识库时携带自身 ID，新知识库会被
	// 原子加入该 key 的绑定集合。Session 主体为 nil。
	CallerAPIKeyID *uuid.UUID
	// SourceType 是知识库内容来源类型；缺省（零值）视为 upload。
	SourceType value.KnowledgeBaseSourceType
	// SourceConfig 是来源配置（飞书：root_token/root_kind/url/cron/next_sync_at）。
	SourceConfig map[string]any
	// SourceConnectionID 是绑定的来源连接；飞书来源必填。
	SourceConnectionID *uuid.UUID
}

// UpdateKnowledgeBaseBasicsInput contains the only mutable basic fields.
type UpdateKnowledgeBaseBasicsInput struct {
	WorkspaceID, KnowledgeBaseID uuid.UUID
	Name, Description            *string
	ActorRole                    value.WorkspaceRole
	Access                       value.ResourceAccess
	IsAPIKey                     bool
}

// KnowledgeBaseBasicsUpdater persists a typed basic-information patch.
type KnowledgeBaseBasicsUpdater interface {
	UpdateBasics(context.Context, UpdateKnowledgeBaseBasicsInput) error
}

// KnowledgeBaseSourcePolicyUpdater persists a typed source delete-policy patch.
// 只更新 source_config.on_delete，保留其它运行期键（root_token/cursor 等）。
type KnowledgeBaseSourcePolicyUpdater interface {
	UpdateSourceDeletePolicy(context.Context, uuid.UUID, uuid.UUID, value.SourceDeletePolicy) error
}

// NewKnowledgeBaseService creates a KnowledgeBase service.
// 可选的 enqueuer 在飞书知识库创建成功后触发首次同步；为 nil 时跳过。
func NewKnowledgeBaseService(binder KnowledgeBaseModelBinder, stores ...KnowledgeBaseCreateStore) *KnowledgeBaseService {
	var store KnowledgeBaseCreateStore
	if len(stores) > 0 {
		store = stores[0]
	} else if candidate, ok := binder.(KnowledgeBaseCreateStore); ok {
		store = candidate
	}
	updater, _ := binder.(KnowledgeBaseBasicsUpdater)
	policyUpdater, _ := binder.(KnowledgeBaseSourcePolicyUpdater)
	return &KnowledgeBaseService{binder: binder, store: store, updater: updater, policyUpdater: policyUpdater}
}

// WithSyncEnqueuer 注入可选的首次同步入队器（飞书 KB 创建后触发）。
// 返回新的 service 以支持链式装配；不影响原实例。
func (s *KnowledgeBaseService) WithSyncEnqueuer(enqueuer KnowledgeBaseSyncEnqueuer, log *slog.Logger) *KnowledgeBaseService {
	clone := *s
	clone.syncEnqueuer = enqueuer
	clone.syncEnqueuerLogger = log
	return &clone
}

// UpdateBasics updates name and/or description without changing content or Generation state.
func (s *KnowledgeBaseService) UpdateBasics(ctx context.Context, input UpdateKnowledgeBaseBasicsInput) (*dto.KnowledgeBase, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: KnowledgeBase lineage 无效", domainerrors.ErrValidation)
	}
	if input.IsAPIKey {
		if input.Access.WorkspaceID != input.WorkspaceID || input.Access.Unrestricted || !input.Access.AllowsKnowledgeBase(input.KnowledgeBaseID) {
			return nil, domainerrors.ErrNotFound
		}
	} else if !input.ActorRole.AtLeast(value.RoleAdmin) {
		return nil, domainerrors.ErrForbidden
	}
	if input.Name == nil && input.Description == nil {
		return nil, fmt.Errorf("%w: 至少提供 name 或 description", domainerrors.ErrValidation)
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: 知识库名称不能为空", domainerrors.ErrValidation)
		}
		input.Name = &name
	}
	if s.updater == nil {
		return nil, fmt.Errorf("%w: KnowledgeBase basics updater 不能为空", domainerrors.ErrValidation)
	}
	if err := s.updater.UpdateBasics(ctx, input); err != nil {
		return nil, err
	}
	resolved, err := s.binder.GetResolved(ctx, input.WorkspaceID, input.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	return dto.KnowledgeBaseFromResolved(resolved), nil
}

// Create creates a KnowledgeBase only when its model is currently selectable.
// 当 SourceType 为飞书来源时，使用 NewKnowledgeBaseWithSource 携带来源信息；
// 事务提交后若注入了 syncEnqueuer，触发首次同步（失败仅记日志，不回滚 KB 创建）。
func (s *KnowledgeBaseService) Create(ctx context.Context, input CreateKnowledgeBaseInput) (*dto.KnowledgeBase, error) {
	kb, err := s.buildKnowledgeBase(input)
	if err != nil {
		return nil, err
	}
	var resolvedModel *model.ResolvedModel
	var retrievalConfig map[string]any
	if s.store != nil {
		resolvedModel, err = s.binder.ResolveSelectable(ctx, input.WorkspaceID, input.EmbeddingModelID)
		if err != nil {
			return nil, err
		}
		root, generation, err := buildInitialKnowledgeBaseState(kb, resolvedModel)
		if err != nil {
			return nil, err
		}
		if err := s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx KnowledgeBaseCreateTx) error {
			// 带 CallerAPIKeyID 时在同一事务内把新知识库原子加入调用 key 的绑定集合。
			return tx.CreateKnowledgeBaseRootGenerationAndBinding(txCtx, kb, root, generation, input.CallerAPIKeyID)
		}); err != nil {
			return nil, err
		}
		retrievalConfig = generation.RetrievalConfig
	} else {
		resolvedModel, err = s.binder.Create(ctx, kb)
		if err != nil {
			return nil, err
		}
	}

	// 事务提交后触发首次同步（仅飞书来源且注入了 enqueuer）。
	// 入队失败只记日志不回滚：KB 已建好，同步可手动重试。
	// 首次同步不强制（Force=false）：复用内容 hash 去重语义。
	if input.SourceType.IsFeishu() && s.syncEnqueuer != nil {
		if _, err := s.syncEnqueuer.EnqueueSync(ctx, input.WorkspaceID, kb.ID, SyncOptions{Force: false}); err != nil {
			if s.syncEnqueuerLogger != nil {
				s.syncEnqueuerLogger.Error("创建飞书知识库后入队首次同步失败",
					"workspace_id", input.WorkspaceID.String(),
					"knowledge_base_id", kb.ID.String(),
					"error", err.Error(),
				)
			}
		}
	}

	return dto.KnowledgeBaseFromResolved(&model.ResolvedKnowledgeBase{
		KnowledgeBase: kb, EmbeddingModel: resolvedModel,
		RetrievalConfig: retrievalConfig,
	}), nil
}

// UpdateSourceDeletePolicy 仅更新 source_config.on_delete，保留其余运行期键。
//
// 仅对飞书来源（feishu_drive/feishu_wiki）知识库生效：on_delete 只在来源同步清理
// 流程中有意义。其它来源返回 ErrValidation。调用方须已通过鉴权（admin/owner）。
func (s *KnowledgeBaseService) UpdateSourceDeletePolicy(ctx context.Context, workspaceID, kbID uuid.UUID, policy value.SourceDeletePolicy) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil {
		return fmt.Errorf("%w: KnowledgeBase lineage 无效", domainerrors.ErrValidation)
	}
	if s.policyUpdater == nil {
		return fmt.Errorf("%w: KnowledgeBase source policy updater 不能为空", domainerrors.ErrValidation)
	}
	resolved, err := s.binder.GetResolved(ctx, workspaceID, kbID)
	if err != nil {
		return err
	}
	if !resolved.KnowledgeBase.SourceType.IsFeishu() {
		return fmt.Errorf("%w: 仅飞书来源知识库支持删除策略配置", domainerrors.ErrValidation)
	}
	return s.policyUpdater.UpdateSourceDeletePolicy(ctx, workspaceID, kbID, policy)
}

// buildKnowledgeBase 根据来源类型构造知识库聚合。
// 飞书来源用 NewKnowledgeBaseWithSource（校验 connection 与 config）；其它/缺省用 NewKnowledgeBase。
// 飞书来源在创建时严格校验 on_delete（缺失补 keep，非法返回 ErrValidation）。
func (s *KnowledgeBaseService) buildKnowledgeBase(input CreateKnowledgeBaseInput) (*model.KnowledgeBase, error) {
	sourceConfig := input.SourceConfig
	if sourceConfig == nil {
		sourceConfig = map[string]any{}
	}
	if input.SourceType.IsFeishu() {
		// 显式 on_delete 走严格解析（非法即拒绝）；缺失则补默认 keep。
		// 历史回读统一用 value.SourceDeletePolicyFromConfig（宽容）。
		resolved := make(map[string]any, len(sourceConfig))
		for k, v := range sourceConfig {
			resolved[k] = v
		}
		if raw, present := resolved["on_delete"]; present {
			policy, err := parseOnDeleteValue(raw)
			if err != nil {
				return nil, err
			}
			resolved["on_delete"] = policy.String()
		} else {
			resolved["on_delete"] = value.SourceDeleteKeep.String()
		}
		return model.NewKnowledgeBaseWithSource(
			input.WorkspaceID, input.Name, input.Description, input.EmbeddingModelID,
			input.ChunkingConfig, map[string]any{}, input.SourceType, resolved, input.SourceConnectionID,
		)
	}
	return model.NewKnowledgeBase(input.WorkspaceID, input.Name, input.Description, input.EmbeddingModelID, input.ChunkingConfig, map[string]any{})
}

// parseOnDeleteValue 把 source_config["on_delete"]（任意类型）归一为字符串后
// 走严格解析。仅当值为字符串且合法时通过；非字符串或非法值返回 ErrValidation。
func parseOnDeleteValue(raw any) (value.SourceDeletePolicy, error) {
	s, ok := raw.(string)
	if !ok {
		return value.SourceDeleteKeep, fmt.Errorf("%w: on_delete 必须是字符串", domainerrors.ErrValidation)
	}
	return value.ParseSourceDeletePolicy(s)
}

// Get gets a KnowledgeBase with its bound model summary.
func (s *KnowledgeBaseService) Get(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.KnowledgeBase, error) {
	if access.WorkspaceID == uuid.Nil || id == uuid.Nil {
		return nil, fmt.Errorf("%w: KnowledgeBase access 参数无效", domainerrors.ErrValidation)
	}
	if !access.Unrestricted && !access.AllowsKnowledgeBase(id) {
		return nil, domainerrors.ErrNotFound
	}
	resolved, err := s.binder.GetResolved(ctx, access.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	return dto.KnowledgeBaseFromResolved(resolved), nil
}

// List lists KnowledgeBases with bound model summaries.
func (s *KnowledgeBaseService) List(ctx context.Context, access value.ResourceAccess) ([]*dto.KnowledgeBase, error) {
	if access.WorkspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: KnowledgeBase access workspace 无效", domainerrors.ErrValidation)
	}
	var allowedIDs []uuid.UUID
	if !access.Unrestricted {
		// Keep a restricted empty binding as an explicit empty slice. A nil
		// allowedIDs value means "no SQL restriction" to the repository and
		// must never be produced for an API-key principal.
		allowedIDs = append([]uuid.UUID{}, access.AllowedKnowledgeBaseIDs...)
	}
	items, err := s.binder.ListResolved(ctx, access.WorkspaceID, allowedIDs)
	if err != nil {
		return nil, err
	}
	result := make([]*dto.KnowledgeBase, 0, len(items))
	for _, item := range items {
		result = append(result, dto.KnowledgeBaseFromResolved(item))
	}
	return result, nil
}
