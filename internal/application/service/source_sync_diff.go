package service

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件实现 spec 5.3/5.4/5.5/6.4 中的纯函数：
//   - localDocView / updateCandidate / syncPlan：本地 diff 视图与同步计划类型；
//   - diff：根据远端快照、本地投影、增量游标与 force 标志产出同步计划（去重 + 删除闸门）；
//   - nodeOutcome / nodeResult / computeSafeCursor：基于成功前缀的安全 cursor watermark。
//
// 这些函数都是无 I/O 的纯函数，可独立单测，供 SourceSyncService（Task 7）编排使用。

// localDocView 是单个本地文档在 diff 视图中的投影。
// 它由 SourceSyncStore（Task 6）从 documents/document_revisions 读出，聚合为 diff 所需的最小字段。
type localDocView struct {
	// DocumentID 是本地文档主键。
	DocumentID uuid.UUID
	// ExternalID 是远端稳定标识（飞书 docx token）；空表示非来源同步文档。
	ExternalID string
	// ContentHash 是当前已接受的最新 source revision 的 SHA-256；用于 hash 判断（Task 7）。
	ContentHash string
	// Status 是文档当前状态。
	Status value.DocumentStatus
	// ActiveRevisionID 是已发布的活跃 revision；可为空。
	ActiveRevisionID *uuid.UUID
	// RevisionNo 是当前最新 source revision 的序号。
	RevisionNo int64
	// RetryRequired 表示文档需要重试：处于 failed、最新 source revision 未成功完成、
	// 或对应 parse Job 入队/执行失败。RetryRequired=true 在 diff 中优先于 cursor，强制 ToUpdate。
	RetryRequired bool
	// DeletedAt 非空表示文档已软删；远端重新出现时进入 ToUpdate 而非 ToAdd。
	DeletedAt *time.Time
}

// updateCandidate 是一个 有/有 匹配对：远端节点 + 对应的本地投影。
type updateCandidate struct {
	Remote model.ExternalNode
	Local  localDocView
}

// syncPlan 是一次 diff 的产物：需要新增、更新、删除的文档集合，以及跳过数和告警。
//
// ToRemove 仅在 snapshot.Complete=true 时填充（删除闸门，spec 5.5）。
type syncPlan struct {
	ToAdd    []model.ExternalNode
	ToUpdate []updateCandidate
	ToRemove []localDocView
	Skipped  int
	Warnings []string
}

// diff 是 spec 5.4 的核心纯函数：输入去重前的远端快照、本地投影、增量游标与 force 标志，
// 输出一次同步计划。它直接读取 snapshot.Complete 决定是否生成 ToRemove，
// 不接受独立的 allowRemoval 参数（避免调用方错误放开删除闸门）。
//
// diff 只处理 HasDocument=true 的文档节点；folder 节点（HasDocument=false）由
// SourceSyncService 的 folder upsert 逻辑（Task 7）单独处理，不在此函数范围内。
// 不支持的文档类型（sheet/bitable 等）的告警与计数是服务层（Task 7）的职责，
// diff 假设调用方传入的快照中需要被 diff 的文档节点都已经过类型过滤。
//
// diff 不做任何 I/O，也不修改 cursor；cursor 的推进由 computeSafeCursor 计算。
func diff(snapshot sourceport.TreeSnapshot, local []localDocView, cursor time.Time, force bool) syncPlan {
	plan := syncPlan{}

	// 1) 对本地投影按 external_id 去重：保留首项用于更新匹配，重复项从删除集合排除并产生 warning。
	//    绝不自动合并业务数据（spec 5.1）。
	localByID, localExcludedFromRemoval := dedupLocal(local, &plan)

	// 2) 遍历远端节点，对文档节点（HasDocument=true）按 token 去重：保留首项（按出现顺序），
	//    重复项丢弃并产生 warning。distinctDocs 保留首次出现顺序，供后续分类。
	seen := make(map[string]bool)
	remoteByID := make(map[string]model.ExternalNode)
	var distinctDocs []model.ExternalNode
	for _, node := range snapshot.Nodes {
		if !node.HasDocument {
			// folder / 非文档节点不进入文档 diff。
			continue
		}
		if node.Token == "" {
			// 空 token 视为数据错误，跳过并告警，避免污染后续匹配。
			plan.Warnings = append(plan.Warnings, "远端文档节点 token 为空，已跳过")
			continue
		}
		if seen[node.Token] {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("远端 token 重复，已丢弃后续项: %s", node.Token))
			continue
		}
		seen[node.Token] = true
		remoteByID[node.Token] = node
		distinctDocs = append(distinctDocs, node)
	}

	// 3) 对每个去重后的远端文档节点分类：有/无 => ToAdd；有/有 => 按 spec 5.4 表判断 ToUpdate / Skipped。
	for _, node := range distinctDocs {
		localDoc, hasLocal := localByID[node.Token]
		if !hasLocal {
			plan.ToAdd = append(plan.ToAdd, node)
			continue
		}
		// 有/有：按优先级 force → RetryRequired → EditTime 零值 → EditTime > cursor → 否则 Skipped。
		if classifyUpdate(force, localDoc, node.EditTime, cursor) {
			plan.ToUpdate = append(plan.ToUpdate, updateCandidate{Remote: node, Local: localDoc})
		} else {
			plan.Skipped++
		}
	}

	// 4) 删除检测：仅完整快照（删除闸门 spec 5.5）。
	if snapshot.Complete {
		for _, doc := range local {
			if doc.ExternalID == "" {
				continue
			}
			if localExcludedFromRemoval[doc.ExternalID] {
				// 本地重复 external_id：绝不自动删除/合并，排除出删除集合。
				continue
			}
			if _, stillRemote := remoteByID[doc.ExternalID]; stillRemote {
				// 远端仍存在，不删除。
				continue
			}
			if doc.DeletedAt != nil {
				// 已软删 => 忽略（spec 5.4 行 3）。
				continue
			}
			plan.ToRemove = append(plan.ToRemove, doc)
		}
	}

	return plan
}

// dedupLocal 对本地投影按 external_id 去重，返回首项索引以及"因重复而必须从删除集合排除"的 external_id 集合。
// 重复项产生 warning；绝不自动合并业务数据（spec 5.1）。
func dedupLocal(local []localDocView, plan *syncPlan) (map[string]localDocView, map[string]bool) {
	byID := make(map[string]localDocView, len(local))
	excludedFromRemoval := make(map[string]bool)
	for _, doc := range local {
		if doc.ExternalID == "" {
			// 非来源同步文档（无 external_id）不参与 diff。
			continue
		}
		if _, exists := byID[doc.ExternalID]; exists {
			// 重复项：保留首项用于更新匹配，但把该 external_id 标记为"从删除集合排除"。
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("本地 external_id 重复，保留首项并排除删除: %s", doc.ExternalID))
			excludedFromRemoval[doc.ExternalID] = true
			continue
		}
		byID[doc.ExternalID] = doc
	}
	return byID, excludedFromRemoval
}

// classifyUpdate 实现 spec 5.4 中 有/有 情况的优先级判定：
//
//	force → RetryRequired → EditTime 零值 → EditTime > cursor → 否则 Skipped。
//
// 返回 true 表示进入 ToUpdate；false 表示进入 Skipped。
func classifyUpdate(force bool, local localDocView, editTime, cursor time.Time) bool {
	switch {
	case force:
		// force 优先于 hash 与 cursor，所有匹配节点强制重新 Fetch + hash 判断。
		return true
	case local.RetryRequired:
		// RetryRequired 优先于 cursor：失败/未完成 revision 必须重试。
		return true
	case editTime.IsZero():
		// EditTime 未知 => 必须 Fetch 后用 hash 判断（spec 5.4）。
		return true
	case editTime.After(cursor):
		// EditTime 比 cursor 新 => ToUpdate。
		return true
	default:
		// EditTime <= cursor 且无重试需求 => Skipped。
		return false
	}
}

// nodeResult 是单个同步节点处理结果的二值分类（spec 6.4）。
type nodeResult int

const (
	// nodeResultSuccess 表示成功 / unchanged / 明确 cursor skip —— 这些结果都推进 watermark。
	nodeResultSuccess nodeResult = iota
	// nodeResultFailure 表示 retryable_failure / oversize / parent failure —— 断开成功前缀。
	nodeResultFailure
)

// nodeOutcome 是单个节点在一次同步中的结果记录，用于 computeSafeCursor 的前缀扫描。
type nodeOutcome struct {
	Token    string
	EditTime time.Time
	Result   nodeResult
}

// computeSafeCursor 实现 spec 6.4 的安全 cursor watermark：
//   - snapshot.Complete=false 时完全不推进，返回 previous；
//   - 完整快照中，文档节点按远端 EditTime 升序形成成功前缀，连续成功（含 unchanged/cursor skip）
//     才推进 watermark，遇到第一个失败/缺少 outcome 即停止；
//   - 同一 EditTime 的节点必须全部成功，才能把 watermark 推进到该时间；
//   - EditTime 零值的节点不参与 watermark（跳过它们，不阻断前缀）；
//   - 计算结果不会让 watermark 倒退（小于 previous 时返回 previous）。
//
// 该函数是纯函数，不做 I/O。
func computeSafeCursor(snapshot sourceport.TreeSnapshot, outcomes []nodeOutcome, previous time.Time) time.Time {
	if !snapshot.Complete {
		return previous
	}

	outcomeByToken := make(map[string]nodeOutcome, len(outcomes))
	for _, o := range outcomes {
		outcomeByToken[o.Token] = o
	}

	// 收集文档节点的非零 EditTime，用于按时间分组做"全部成功"判定。
	type timed struct {
		token    string
		editTime time.Time
	}
	var docs []timed
	for _, node := range snapshot.Nodes {
		if !node.HasDocument {
			continue
		}
		if node.EditTime.IsZero() {
			// 零值节点不参与 watermark（spec 6.4），直接跳过，不阻断前缀。
			continue
		}
		docs = append(docs, timed{token: node.Token, editTime: node.EditTime})
	}
	if len(docs) == 0 {
		return previous
	}

	// 按 EditTime 升序排序，保证前缀扫描以时间为序。
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].editTime.Before(docs[j].editTime)
	})

	// 前缀扫描：按 EditTime 分组，每组必须"全部已知 outcome 且全部成功"才推进到该时间。
	// 一旦某组失败/缺少 outcome，立即停止推进。
	watermark := previous
	for i := 0; i < len(docs); {
		j := i
		// 找到同一 EditTime 组的右端（半开区间 [i, j)）。
		for j < len(docs) && docs[j].editTime.Equal(docs[i].editTime) {
			j++
		}

		groupFailed := false
		for k := i; k < j; k++ {
			outcome, ok := outcomeByToken[docs[k].token]
			if !ok {
				// 缺少 outcome => 视为未知，前缀在此停止。
				groupFailed = true
				break
			}
			if outcome.Result != nodeResultSuccess {
				groupFailed = true
				break
			}
		}

		if groupFailed {
			break
		}

		// 整组成功：把 watermark 推进到该组的 EditTime。
		watermark = docs[i].editTime
		i = j
	}

	// watermark 不得倒退（例如 previous 已是未来时间，或计算出的前缀更早）。
	if watermark.Before(previous) {
		return previous
	}
	return watermark
}
