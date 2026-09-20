package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件承载 web 内容源（kb_sources.kind='web'）的管理面用例（v1.3.0 spec §9.2）：
// 创建（typed config 校验 + 连接校验 + 每库源数钳制 + 首次同步入队）、列表/详情
// （含每源页面/失败计数）、PATCH（键白名单 + 不可变键拒绝 + 深拷贝合并 + 全量重校验，
// 成功即入队同步）、启停、手动重跑（同源单 active 由 T9 入队合并保证）与删除两分支
// （keep=断链保页；remove=走既有文档删除路径逐页删除）。
// REST handler（T13）只做协议转换；本层不感知 HTTP/MCP。

// SourcePageCounts 是单个内容源的页面计数投影（documents.kb_source_id 聚合）。
type SourcePageCounts struct {
	// PageCount 是该源名下未软删页面文档数。
	PageCount int
	// FailedPageCount 是其中处于 failed 状态的页面数（Console 失败告警用）。
	FailedPageCount int
}

// WebSourceView 是源列表/详情的读投影：源行 + 每源页面计数。
type WebSourceView struct {
	Source          *model.KbSource
	PageCount       int
	FailedPageCount int
}

// KbSourceStore 是源管理所需的持久化契约（由 infrastructure/db.KbSourceDBRepository 实现）。
type KbSourceStore interface {
	Create(ctx context.Context, source *model.KbSource) error
	Get(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) (*model.KbSource, error)
	List(ctx context.Context, workspaceID, kbID uuid.UUID) ([]*model.KbSource, error)
	CountByKB(ctx context.Context, workspaceID, kbID uuid.UUID) (int, error)
	Update(ctx context.Context, source *model.KbSource) error
	SoftDelete(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) error
	CountPagesBySource(ctx context.Context, workspaceID, kbID uuid.UUID) (map[uuid.UUID]SourcePageCounts, error)
	ListDocumentIDsBySource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) ([]uuid.UUID, error)
	DetachSourceDocuments(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) (int64, error)
	// CountActiveBySource 统计源进行中的源级 source_sync 任务数（pending/running），
	// 删除入口的在飞守卫用（T10 评审 Minor-2）。
	CountActiveBySource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) (int, error)
}

// KbSourceSyncEnqueuer 把源级 source_sync 同步入队。
// 由 *SourceSyncService 实现（EnqueueSyncForSource，同源单 active 在其内部
// RequestSourceSyncForSource 源行锁事务内保证）；为 nil 时创建/PATCH 跳过入队。
type KbSourceSyncEnqueuer interface {
	EnqueueSyncForSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, options SyncOptions) (*model.Job, error)
}

// SourceDocumentRemover 以 remove 策略删除一批文档并入队清理任务。
// 由 *SourceSyncService 实现（RemoveSourceDocuments，与来源同步删除路径同语义）。
type SourceDocumentRemover interface {
	RemoveSourceDocuments(ctx context.Context, workspaceID, kbID uuid.UUID, documentIDs []uuid.UUID) (int, error)
}

// KbSourceServiceDeps 注入源管理用例的全部依赖。
type KbSourceServiceDeps struct {
	// KBs 复用来源同步的只读知识库端口（Get 校验 KB 存在且未删）。
	KBs KnowledgeBaseSyncRepository
	// Store 是 kb_sources 的持久化端口。
	Store KbSourceStore
	// Connections 用于创建时校验 fetch.connection_id（provider 匹配 + 同 workspace active）。
	Connections SourceConnectionRepository
	// SyncEnqueuer 入队首次/PATCH 后同步；为 nil 时跳过入队（仅测试装配需要）。
	SyncEnqueuer KbSourceSyncEnqueuer
	// Remover 执行 remove 删除分支的页面删除；为 nil 时 remove 分支报装配错误。
	Remover SourceDocumentRemover
	// Limits 是 web 源 config 解析的进程级天花板（config.web_source）。
	Limits value.WebSourceLimits
	// MaxSourcesPerKB 是单库可挂源数上限（config.web_source.max_sources_per_kb）。
	MaxSourcesPerKB int
	Logger          Logger
}

// KbSourceService 管理 web 内容源的生命周期（创建/查询/PATCH/启停/重跑/删除）。
type KbSourceService struct {
	kbs             KnowledgeBaseSyncRepository
	store           KbSourceStore
	conns           SourceConnectionRepository
	enqueuer        KbSourceSyncEnqueuer
	remover         SourceDocumentRemover
	limits          value.WebSourceLimits
	maxSourcesPerKB int
	logger          Logger
}

// NewKbSourceService 构造源管理服务。
func NewKbSourceService(deps KbSourceServiceDeps) *KbSourceService {
	logger := deps.Logger
	if logger == nil {
		logger = noopLogger{}
	}
	maxSources := deps.MaxSourcesPerKB
	if maxSources <= 0 {
		maxSources = defaultWebSourceMaxSourcesPerKB
	}
	return &KbSourceService{
		kbs: deps.KBs, store: deps.Store, conns: deps.Connections,
		enqueuer: deps.SyncEnqueuer, remover: deps.Remover,
		limits: deps.Limits, maxSourcesPerKB: maxSources, logger: logger,
	}
}

// defaultWebSourceMaxSourcesPerKB 是 max_sources_per_kb 的兜底默认
// （与 config.web_source.max_sources_per_kb 缺省一致）。
const defaultWebSourceMaxSourcesPerKB = 10

// CreateWebSourceInput 是创建 web 内容源的输入。
type CreateWebSourceInput struct {
	WorkspaceID uuid.UUID
	// KnowledgeBaseID 必须指向存在且未删的知识库。
	KnowledgeBaseID uuid.UUID
	Name            string
	// Config 是 kb_sources.config 的原始 map，经 value.ParseWebSourceConfig 全量校验。
	Config map[string]any
	// Status 为空时取 active。
	Status value.KbSourceStatus
}

// CreateWebSource 创建一个 web 内容源（spec §9.2 POST）：
// KB 存在且未删 → 每库源数钳制 → config typed 校验（天花板随部署注入）→
// fetch.connection_id 校验（provider 匹配 + 同 workspace active）→ 落库 →
// 入队首次同步（失败只记日志不回滚：源已建好，同步可手动重试，与建 KB 惯例一致）。
// 返回创建的源与首次同步的 Job（入队跳过/失败时为 nil）。
func (s *KbSourceService) CreateWebSource(ctx context.Context, input CreateWebSourceInput) (*model.KbSource, *model.Job, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil {
		return nil, nil, fmt.Errorf("%w: workspace_id/knowledge_base_id 不能为空", domainerrors.ErrValidation)
	}
	if s.kbs == nil || s.store == nil {
		return nil, nil, fmt.Errorf("%w: 源管理依赖未配置", domainerrors.ErrValidation)
	}
	if _, err := s.kbs.Get(ctx, input.WorkspaceID, input.KnowledgeBaseID); err != nil {
		return nil, nil, fmt.Errorf("读取知识库失败: %w", err)
	}
	if err := s.ensureSourceQuota(ctx, input.WorkspaceID, input.KnowledgeBaseID); err != nil {
		return nil, nil, err
	}

	config := input.Config
	if config == nil {
		config = map[string]any{}
	}
	typed, err := value.ParseWebSourceConfig(config, s.limits)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validateFetchConnection(ctx, input.WorkspaceID, typed.Fetch); err != nil {
		return nil, nil, err
	}

	source, err := model.NewKbSource(model.NewKbSourceInput{
		WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		Kind: value.KbSourceKindWeb, Name: input.Name,
		Status: input.Status, Config: config,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := s.store.Create(ctx, source); err != nil {
		if errors.Is(err, domainerrors.ErrConflict) {
			// uq_kb_sources_active_name：同库活源名称唯一（软删后名字释放）。
			return nil, nil, fmt.Errorf("%w: 同名内容源已存在（%s）", domainerrors.ErrConflict, source.Name)
		}
		return nil, nil, err
	}

	job := s.enqueueSyncBestEffort(ctx, input.WorkspaceID, input.KnowledgeBaseID, source.ID, "创建源后首次同步")
	return source, job, nil
}

// ListWebSources 列出 KB 下全部未软删源，附每源页面/失败计数。
func (s *KbSourceService) ListWebSources(ctx context.Context, workspaceID, kbID uuid.UUID) ([]WebSourceView, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id/knowledge_base_id 不能为空", domainerrors.ErrValidation)
	}
	if s.store == nil {
		return nil, fmt.Errorf("%w: 源管理依赖未配置", domainerrors.ErrValidation)
	}
	sources, err := s.store.List(ctx, workspaceID, kbID)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.CountPagesBySource(ctx, workspaceID, kbID)
	if err != nil {
		return nil, err
	}
	views := make([]WebSourceView, 0, len(sources))
	for _, source := range sources {
		pageCounts := counts[source.ID]
		views = append(views, WebSourceView{
			Source: source, PageCount: pageCounts.PageCount, FailedPageCount: pageCounts.FailedPageCount,
		})
	}
	return views, nil
}

// GetWebSource 读取单个源的详情（含计数）；不存在或已软删返回 ErrNotFound。
func (s *KbSourceService) GetWebSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) (*WebSourceView, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id/knowledge_base_id/source_id 不能为空", domainerrors.ErrValidation)
	}
	if s.store == nil {
		return nil, fmt.Errorf("%w: 源管理依赖未配置", domainerrors.ErrValidation)
	}
	source, err := s.store.Get(ctx, workspaceID, kbID, sourceID)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.CountPagesBySource(ctx, workspaceID, kbID)
	if err != nil {
		return nil, err
	}
	pageCounts := counts[sourceID]
	return &WebSourceView{
		Source: source, PageCount: pageCounts.PageCount, FailedPageCount: pageCounts.FailedPageCount,
	}, nil
}

// webSourcePatchTopLevelKeys 是 PATCH 允许出现的 config 顶层键（spec §9.2 白名单）。
// 子对象内部键的合法性（含未知键拒绝）由合并后的 value.ParseWebSourceConfig 全量重校验。
var webSourcePatchTopLevelKeys = map[string]struct{}{
	"urls": {}, "crawl": {}, "sitemap": {}, "fetch": {}, "extract": {}, "sync": {}, "on_delete": {},
}

// UpdateWebSourceInput 是 PATCH 源的输入：name/status 键 + partial config。
type UpdateWebSourceInput struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	SourceID        uuid.UUID
	// Name 非空时改名（trim 后不得为空）。
	Name *string
	// Status 非空时启停（见 SetWebSourceStatus）。
	Status *value.KbSourceStatus
	// Config 是 partial config（nil 表示不改 config）；urls 为整体替换语义。
	Config map[string]any
}

// UpdateWebSource 按 PATCH 语义更新源（spec §9.2）：
//   - 顶层键白名单校验（未知键/运行态键拒绝）；
//   - 不可变键（mode/crawl.seed_url/sitemap.url/fetch.engine/fetch.connection_id）
//     经 value.RejectImmutableWebSourceConfigKeys 拒绝——变更即换源身份，应新建源；
//   - Config 拷贝语义：深拷贝旧 config 再合并 partial，禁止原地改
//     （KbSource.Config 按引用保存，原地改会波及已构造对象，T4 评审移交）；
//   - 合并后全量 ParseWebSourceConfig 重校验（覆盖子对象未知键与跨字段约束）；
//   - PATCH 成功即入队一次同步（best-effort：入队失败由调度器恢复，不回滚配置）。
func (s *KbSourceService) UpdateWebSource(ctx context.Context, input UpdateWebSourceInput) (*model.KbSource, *model.Job, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || input.SourceID == uuid.Nil {
		return nil, nil, fmt.Errorf("%w: 源 lineage 不能为空", domainerrors.ErrValidation)
	}
	if input.Name == nil && input.Status == nil && input.Config == nil {
		return nil, nil, fmt.Errorf("%w: 至少提供 name/status/config 之一", domainerrors.ErrValidation)
	}
	if s.store == nil {
		return nil, nil, fmt.Errorf("%w: 源管理依赖未配置", domainerrors.ErrValidation)
	}
	source, err := s.store.Get(ctx, input.WorkspaceID, input.KnowledgeBaseID, input.SourceID)
	if err != nil {
		return nil, nil, err
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, nil, fmt.Errorf("%w: 源名称不能为空", domainerrors.ErrValidation)
		}
		source.Name = name
	}
	if input.Status != nil {
		if err := input.Status.Validate(); err != nil {
			return nil, nil, err
		}
		source.Status = *input.Status
	}
	if input.Config != nil {
		if err := validateWebSourcePatchConfig(input.Config); err != nil {
			return nil, nil, err
		}
		if err := value.RejectImmutableWebSourceConfigKeys(input.Config); err != nil {
			return nil, nil, err
		}
		merged := mergeWebSourceConfig(source.Config, input.Config)
		if _, err := value.ParseWebSourceConfig(merged, s.limits); err != nil {
			return nil, nil, err
		}
		source.Config = merged
	}

	if err := s.store.Update(ctx, source); err != nil {
		if errors.Is(err, domainerrors.ErrConflict) {
			return nil, nil, fmt.Errorf("%w: 同名内容源已存在（%s）", domainerrors.ErrConflict, source.Name)
		}
		return nil, nil, err
	}
	job := s.enqueueSyncBestEffort(ctx, input.WorkspaceID, input.KnowledgeBaseID, source.ID, "PATCH 源后同步")
	return source, job, nil
}

// SetWebSourceStatus 启停源（spec §9.2 PATCH status 键的独立入口）。
// disabled 源不参与调度（ListDueWebSources 过滤 status=active）；已入队任务由
// worker 执行时幂等跳过。同值重复设置幂等成功。
func (s *KbSourceService) SetWebSourceStatus(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, status value.KbSourceStatus) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return fmt.Errorf("%w: 源 lineage 不能为空", domainerrors.ErrValidation)
	}
	if err := status.Validate(); err != nil {
		return err
	}
	if s.store == nil {
		return fmt.Errorf("%w: 源管理依赖未配置", domainerrors.ErrValidation)
	}
	source, err := s.store.Get(ctx, workspaceID, kbID, sourceID)
	if err != nil {
		return err
	}
	source.Status = status
	return s.store.Update(ctx, source)
}

// SyncWebSource 手动触发一次同步（spec §9.2 POST .../sync {"force"}）。
// 直接委托 T9 的 EnqueueSyncForSource：同源单 active（reuse-or-create）、force latch、
// 停用/软删源拒绝均已在其内部源行锁事务中保证，本方法只是调用方。
// 源不存在/已软删返回 ErrNotFound。
func (s *KbSourceService) SyncWebSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, force bool) (*model.Job, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return nil, fmt.Errorf("%w: 源 lineage 不能为空", domainerrors.ErrValidation)
	}
	if s.enqueuer == nil {
		return nil, fmt.Errorf("%w: 源同步入队器未配置", domainerrors.ErrValidation)
	}
	// 存在性预检：把「源不存在」与入队依赖错误区分开（错误链均为领域错误）。
	if s.store != nil {
		if _, err := s.store.Get(ctx, workspaceID, kbID, sourceID); err != nil {
			return nil, err
		}
	}
	return s.enqueuer.EnqueueSyncForSource(ctx, workspaceID, kbID, sourceID, SyncOptions{Force: force})
}

// DeleteWebSource 删除源并按 pages 策略处置页面（spec §9.2 DELETE ?pages=keep|remove）：
//   - keep（默认）：断链置 NULL（documents.kb_source_id = NULL，页面保留可检索）+ 源行软删；
//   - remove：走既有文档删除路径（DeleteSourceDocument remove 策略 + 清理任务）逐页删除 +
//     源行软删；任一页删除失败即中止（源保持未删，可重试；已删页面重试时不再出现）。
//
// 在飞守卫（T10 评审 Minor-2）：存在进行中的源级同步任务时拒绝删除——remove 分支
// 软删源后再新建的页面会挂在已删源下残留（remove 意图未兑现）。该检查是
// best-effort：检查与删除之间仍可能入队新任务，此时任务按下方在途语义终态化。
//
// 在途任务语义（显式确认）：源软删后 GetSyncKbSource 拒绝软删行（T8 已修），在飞任务
// 推进时读源失败以 failed 终态结束、不再续跑（worker 判 ErrNotFound SkipRetry）；仍 pending
// 的源级 source_sync Job 不主动取消，按现状留着——worker 执行时同样以 ErrNotFound 终结，
// 不产生重试风暴。
func (s *KbSourceService) DeleteWebSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, pages string) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return fmt.Errorf("%w: 源 lineage 不能为空", domainerrors.ErrValidation)
	}
	policy := value.SourceDeleteKeep
	if pages != "" {
		parsed, err := value.ParseSourceDeletePolicy(pages)
		if err != nil {
			return err
		}
		policy = parsed
	}
	if s.store == nil {
		return fmt.Errorf("%w: 源管理依赖未配置", domainerrors.ErrValidation)
	}
	// 存在性预检（含已软删拒绝）：删除幂等由 SoftDelete 的未删行条件保证。
	if _, err := s.store.Get(ctx, workspaceID, kbID, sourceID); err != nil {
		return err
	}
	// 在飞守卫：keep/remove 两分支通用。冲突（409）语义——稍后重试可成功，
	// 非请求本身非法。
	if active, err := s.store.CountActiveBySource(ctx, workspaceID, kbID, sourceID); err != nil {
		return err
	} else if active > 0 {
		return fmt.Errorf("%w: 内容源有 %d 个正在进行的同步任务，请稍后重试", domainerrors.ErrConflict, active)
	}

	switch policy {
	case value.SourceDeleteKeep:
		if _, err := s.store.DetachSourceDocuments(ctx, workspaceID, kbID, sourceID); err != nil {
			return err
		}
	case value.SourceDeleteRemove:
		if s.remover == nil {
			return fmt.Errorf("%w: 源页面删除器未配置", domainerrors.ErrValidation)
		}
		documentIDs, err := s.store.ListDocumentIDsBySource(ctx, workspaceID, kbID, sourceID)
		if err != nil {
			return err
		}
		if len(documentIDs) > 0 {
			deleted, err := s.remover.RemoveSourceDocuments(ctx, workspaceID, kbID, documentIDs)
			if err != nil {
				return err
			}
			if deleted < len(documentIDs) {
				return fmt.Errorf("删除内容源页面失败: %d/%d 未完成，可重试", len(documentIDs)-deleted, len(documentIDs))
			}
		}
	default:
		return fmt.Errorf("%w: 不支持的页面处置策略 %q", domainerrors.ErrValidation, policy)
	}
	return s.store.SoftDelete(ctx, workspaceID, kbID, sourceID)
}

// ensureSourceQuota 校验每库源数上限（config.web_source.max_sources_per_kb，默认 10）。
// 超限返回 ErrValidation（与 crawl.max_pages 天花板同口径：显式拒绝而非静默截断）。
// count-then-insert 非原子，并发创建下为 best-effort 软上限：管理面仅 admin/owner
// 可达、偶发超限失败模式无害（源可删），不为此引入源行锁事务。
func (s *KbSourceService) ensureSourceQuota(ctx context.Context, workspaceID, kbID uuid.UUID) error {
	count, err := s.store.CountByKB(ctx, workspaceID, kbID)
	if err != nil {
		return err
	}
	if count >= s.maxSourcesPerKB {
		return fmt.Errorf("%w: 该知识库内容源数已达上限 %d（调高 config.yaml web_source.max_sources_per_kb 或先删除源）",
			domainerrors.ErrValidation, s.maxSourcesPerKB)
	}
	return nil
}

// webEngineAllowedProviders 返回引擎允许的连接 provider 集合；http 无需连接返回 nil。
func webEngineAllowedProviders(engine value.WebSourceFetchEngine) []value.SourceProvider {
	switch engine {
	case value.WebSourceFetchEngineRender:
		return []value.SourceProvider{value.SourceProviderBrowser}
	case value.WebSourceFetchEngineFirecrawl:
		return []value.SourceProvider{value.SourceProviderFirecrawl}
	case value.WebSourceFetchEngineJina:
		return []value.SourceProvider{value.SourceProviderJina}
	case value.WebSourceFetchEngineAuto:
		// auto 的升级目标可以是任意外部引擎连接（spec §6.2）。
		return []value.SourceProvider{value.SourceProviderBrowser, value.SourceProviderFirecrawl, value.SourceProviderJina}
	default:
		return nil
	}
}

// validateFetchConnection 校验 fetch 配置引用的连接（spec §9.2 创建校验）：
// http 引擎必须无连接（ParseWebSourceConfig 已保证）；其余引擎的 connection_id
// 必须指向同 workspace、active 状态且 provider 与引擎匹配的连接。
func (s *KbSourceService) validateFetchConnection(ctx context.Context, workspaceID uuid.UUID, fetch value.WebSourceFetchConfig) error {
	allowed := webEngineAllowedProviders(fetch.Engine)
	if len(allowed) == 0 {
		return nil
	}
	if fetch.ConnectionID == "" {
		return fmt.Errorf("%w: fetch.engine=%s 需要 fetch.connection_id", domainerrors.ErrValidation, fetch.Engine)
	}
	if s.conns == nil {
		return fmt.Errorf("%w: 来源连接依赖未配置", domainerrors.ErrValidation)
	}
	connectionID, err := uuid.Parse(fetch.ConnectionID)
	if err != nil {
		return fmt.Errorf("%w: fetch.connection_id 不是合法 UUID", domainerrors.ErrValidation)
	}
	conn, err := s.conns.Get(ctx, workspaceID, connectionID)
	if err != nil {
		if errors.Is(err, domainerrors.ErrNotFound) {
			return fmt.Errorf("%w: fetch.connection_id 指向的连接不存在", domainerrors.ErrValidation)
		}
		return err
	}
	if conn.Status != "active" {
		return fmt.Errorf("%w: fetch.connection_id 指向的连接已停用", domainerrors.ErrValidation)
	}
	provider := value.SourceProvider(strings.ToLower(strings.TrimSpace(conn.Provider)))
	for _, allowedProvider := range allowed {
		if provider == allowedProvider {
			return nil
		}
	}
	return fmt.Errorf("%w: fetch.engine=%s 不允许 %s 连接（允许：%s）",
		domainerrors.ErrValidation, fetch.Engine, provider, joinProviders(allowed))
}

func joinProviders(providers []value.SourceProvider) string {
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		names = append(names, string(provider))
	}
	return strings.Join(names, "/")
}

// validateWebSourcePatchConfig 校验 PATCH partial config 的顶层键白名单
// （spec §9.2）。子对象内部键交由合并后的全量重校验拒绝。
func validateWebSourcePatchConfig(partial map[string]any) error {
	for key := range partial {
		if _, allowed := webSourcePatchTopLevelKeys[key]; !allowed {
			return fmt.Errorf("%w: PATCH 键 %q 不允许修改", domainerrors.ErrValidation, key)
		}
	}
	return nil
}

// mergeWebSourceConfig 深拷贝 old 后合并 partial：map 递归合并、数组与标量整体替换
// （urls 语义为整体替换，spec §9.2）。禁止原地改 old——KbSource.Config 按引用保存，
// 原地合并会波及持有同一 map 的已构造对象（T4 评审移交）。
func mergeWebSourceConfig(old, partial map[string]any) map[string]any {
	merged := make(map[string]any, len(old)+len(partial))
	for key, val := range old {
		merged[key] = deepCopyConfigValue(val)
	}
	for key, val := range partial {
		if partialMap, partialIsMap := val.(map[string]any); partialIsMap {
			if baseMap, baseIsMap := merged[key].(map[string]any); baseIsMap {
				merged[key] = mergeWebSourceConfig(baseMap, partialMap)
				continue
			}
		}
		merged[key] = deepCopyConfigValue(val)
	}
	return merged
}

// deepCopyConfigValue 深拷贝 config 值（map/slice 递归，标量不可变直接返回）。
func deepCopyConfigValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, val := range typed {
			out[key] = deepCopyConfigValue(val)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, val := range typed {
			out[i] = deepCopyConfigValue(val)
		}
		return out
	default:
		return v
	}
}

// enqueueSyncBestEffort 入队一次源同步；失败只记日志不回滚（Job 已落库 pending，
// 由 Meta Scheduler 下个 Tick 恢复派发）。返回 Job；未配置入队器或入队失败返回 nil。
func (s *KbSourceService) enqueueSyncBestEffort(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, reason string) *model.Job {
	if s.enqueuer == nil {
		return nil
	}
	job, err := s.enqueuer.EnqueueSyncForSource(ctx, workspaceID, kbID, sourceID, SyncOptions{Force: false})
	if err != nil {
		s.logger.Warn(reason+"入队失败，等待调度器恢复",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"kb_source_id", sourceID.String(),
			"error", err.Error(),
		)
		return nil
	}
	return job
}
