package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件承载 document_ingest url 入口（v1.3.0 spec §9.3）的源选择与并入用例：
// 要求目标 KB 恰有一个 mode=urls 且 active 的 web 源——没有则返回 validation error
// 提示先创建，多个则列出候选让调用方指定（本版不加 source 选择参数，保持工具面最小）。
// 判断依据是「KB 是否恰有一个 urls 模式 active 源」，与 KB 自身的 source_type 无关
// （upload KB 同样可以有 web 源）。URL 规范化后并入该源现有 urls 列表，复用 T10 的
// UpdateWebSource（PATCH urls 整体替换 + 全量重校验 + 成功即入队）。

// AddURLToWebSource 把 rawURL 并入目标 KB 恰有的一个 urls 模式 active 网站源并触发同步。
// 返回被更新的源与本次入队的同步 Job（入队 best-effort：未配置入队器或入队失败时为 nil，
// 由调度器恢复派发）。
func (s *KbSourceService) AddURLToWebSource(ctx context.Context, workspaceID, kbID uuid.UUID, rawURL string) (*model.KbSource, *model.Job, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil {
		return nil, nil, fmt.Errorf("%w: workspace_id/knowledge_base_id 不能为空", domainerrors.ErrValidation)
	}
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, nil, fmt.Errorf("%w: url 不能为空", domainerrors.ErrValidation)
	}
	if s.store == nil {
		return nil, nil, fmt.Errorf("%w: 源管理依赖未配置", domainerrors.ErrValidation)
	}
	sources, err := s.store.List(ctx, workspaceID, kbID)
	if err != nil {
		return nil, nil, err
	}
	source, err := selectURLsWebSource(sources)
	if err != nil {
		return nil, nil, err
	}
	normalized, err := value.NormalizeWebSourceURI(trimmed)
	if err != nil {
		return nil, nil, err
	}
	typed, err := value.ParseWebSourceConfig(source.Config, s.limits)
	if err != nil {
		return nil, nil, fmt.Errorf("读取源 urls 配置失败: %w", err)
	}
	// PATCH urls 为整体替换语义（spec §9.2）：合并后的列表 = 现有列表 + 新 URL；
	// 已存在（规范化相等）时列表不变，仍 PATCH 以触发一次同步（ParseWebSourceConfig
	// 拒绝规范化后重复项，去重是 PATCH 成功的前提）。
	urls := appendURLIfAbsent(typed.URLs, normalized)
	return s.UpdateWebSource(ctx, UpdateWebSourceInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, SourceID: source.ID,
		Config: map[string]any{"urls": urls},
	})
}

// selectURLsWebSource 从 KB 的未软删源中选出唯一的 mode=urls 且 active 的 web 源：
//   - 恰一个 → 返回；
//   - 零个 → ErrValidation：区分「还没有 web 源」（提示先创建）与「有 web 源但不满足
//     mode=urls 且 active」（列出所见源，帮助调用方理解现状）；
//   - 多个 → ErrValidation 列出候选（名称 + ID），让调用方先收敛到唯一源。
func selectURLsWebSource(sources []*model.KbSource) (*model.KbSource, error) {
	webSources := make([]*model.KbSource, 0, len(sources))
	candidates := make([]*model.KbSource, 0, len(sources))
	for _, source := range sources {
		if source.Kind != value.KbSourceKindWeb {
			continue
		}
		webSources = append(webSources, source)
		if source.Status == value.KbSourceStatusActive && modeOf(source.Config) == value.WebSourceModeURLs {
			candidates = append(candidates, source)
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		if len(webSources) == 0 {
			return nil, fmt.Errorf("%w: 目标知识库还没有网站内容源，请先创建一个 mode=urls 的网站源（REST POST .../knowledge-bases/{id}/web-sources）",
				domainerrors.ErrValidation)
		}
		return nil, fmt.Errorf("%w: 目标知识库需要恰有一个 mode=urls 且 active 的网站源（现有源：%s）",
			domainerrors.ErrValidation, describeWebSources(webSources))
	default:
		return nil, fmt.Errorf("%w: 目标知识库有 %d 个 mode=urls 的 active 网站源，请先只保留一个再调用（候选：%s）",
			domainerrors.ErrValidation, len(candidates), describeWebSources(candidates))
	}
}

// describeWebSources 生成「名称(ID, 模式, 状态)」的确定性列表（按名称 + ID 排序，
// 不依赖 store 的遍历顺序），用于错误信息里的现状/候选展示。
func describeWebSources(sources []*model.KbSource) string {
	sorted := append([]*model.KbSource(nil), sources...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].ID.String() < sorted[j].ID.String()
	})
	parts := make([]string, 0, len(sorted))
	for _, source := range sorted {
		parts = append(parts, fmt.Sprintf("%s(%s, %s, %s)", source.Name, source.ID, modeOf(source.Config), source.Status))
	}
	return strings.Join(parts, "、")
}

// appendURLIfAbsent 在现有 urls（已规范化）基础上追加新规范化 URL；已存在时不重复。
// 返回新切片，不改动入参。
func appendURLIfAbsent(existing []string, normalized string) []string {
	urls := append([]string(nil), existing...)
	for _, u := range urls {
		if u == normalized {
			return urls
		}
	}
	return append(urls, normalized)
}
