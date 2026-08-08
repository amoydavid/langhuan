package service

import (
	"testing"
	"time"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
	"github.com/google/uuid"
)

// 这些测试覆盖 spec 5.4 的 diff 规则表与 spec 6.4 的安全 cursor 计算。
// 它们都是纯函数，不依赖任何 I/O。

// ---- 测试辅助构造 ----

// diffNamespace 是测试专用的 uuid v5 命名空间，保证 diffLocalView 生成的 DocumentID 稳定且唯一。
var diffNamespace = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("langhuan-diff-test"))

// diffDocNode 构造一个 HasDocument=true 的 docx 远端节点。
func diffDocNode(token string, editTime time.Time) model.ExternalNode {
	return model.ExternalNode{
		Token:       token,
		ObjType:     "docx",
		HasDocument: true,
		Title:       token,
		EditTime:    editTime,
	}
}

// diffFolderNode 构造一个 folder 节点（HasDocument=false），不应进入文档 diff。
func diffFolderNode(token string, editTime time.Time) model.ExternalNode {
	return model.ExternalNode{
		Token:       token,
		ObjType:     "folder",
		HasDocument: false,
		Title:       token,
		EditTime:    editTime,
	}
}

// diffLocalView 构造一个未删除、非重试的本地文档投影。
func diffLocalView(externalID string) LocalDocView {
	return LocalDocView{
		DocumentID:  diffViewUUID(externalID),
		ExternalID:  externalID,
		ContentHash: "hash-" + externalID,
		Status:      value.DocumentStatusReady,
	}
}

// diffViewUUID 从 externalID 派生一个稳定 uuid，仅用于测试。
func diffViewUUID(externalID string) uuid.UUID {
	return uuid.NewSHA1(diffNamespace, []byte(externalID))
}

// diffFailedView 构造一个 RetryRequired=true 的本地文档投影（已失败需重试）。
func diffFailedView(externalID string) LocalDocView {
	v := diffLocalView(externalID)
	v.Status = value.DocumentStatusFailed
	v.RetryRequired = true
	return v
}

// diffDeletedView 构造一个已软删的本地文档投影。
func diffDeletedView(externalID string) LocalDocView {
	v := diffLocalView(externalID)
	v.Status = value.DocumentStatusDeleted
	t := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	v.DeletedAt = &t
	return v
}

var (
	cursorOld = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	editNew   = time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)
	editOld   = time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
)

// ---- diff 规则测试（覆盖 spec 5.4 表每一行） ----

func TestDiffRules(t *testing.T) {
	type counts struct {
		add, update, remove, skipped, warnings int
	}
	cases := []struct {
		name     string
		snapshot sourceport.TreeSnapshot
		local    []LocalDocView
		cursor   time.Time
		force    bool
		want     counts
	}{
		{
			name:     "empty complete snapshot + empty local => 全空",
			snapshot: sourceport.TreeSnapshot{Complete: true},
			want:     counts{},
		},
		{
			name:     "empty incomplete snapshot + empty local => 全空且无删除",
			snapshot: sourceport.TreeSnapshot{Complete: false},
			want:     counts{},
		},
		// 行 1: 有/无 => ToAdd
		{
			name:     "远端有 / 本地无 => ToAdd",
			snapshot: sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("remote", editNew)}, Complete: true},
			want:     counts{add: 1},
		},
		{
			name:     "远端有 / 本地无（incomplete）仍可 ToAdd",
			snapshot: sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("remote", editNew)}, Complete: false},
			want:     counts{add: 1},
		},
		// 行 2: 无/有且未删除 + Complete => ToRemove
		{
			name:     "远端无 / 本地有未删除 + Complete => ToRemove",
			snapshot: sourceport.TreeSnapshot{Nodes: nil, Complete: true},
			local:    []LocalDocView{diffLocalView("missing")},
			want:     counts{remove: 1},
		},
		// 删除闸门: Complete=false 时 ToRemove 必须为空
		{
			name:     "远端无 / 本地有未删除 + Incomplete => 不删除",
			snapshot: sourceport.TreeSnapshot{Nodes: nil, Complete: false},
			local:    []LocalDocView{diffLocalView("missing")},
			want:     counts{},
		},
		// 行 3: 无/有且已删除 => 忽略（不计入任何集合）
		{
			name:     "远端无 / 本地已软删 => 忽略",
			snapshot: sourceport.TreeSnapshot{Nodes: nil, Complete: true},
			local:    []LocalDocView{diffDeletedView("gone")},
			want:     counts{},
		},
		// 行: force=true 优先，即使 cursor 落后也更新
		{
			name:     "force=true 即使 EditTime <= cursor 也 ToUpdate",
			snapshot: sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("same", editOld)}, Complete: true},
			local:    []LocalDocView{diffLocalView("same")},
			cursor:   cursorOld,
			force:    true,
			want:     counts{update: 1},
		},
		// 行: RetryRequired=true 优先于 cursor
		{
			name:     "RetryRequired=true 即使 EditTime <= cursor 也 ToUpdate",
			snapshot: sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("same", editOld)}, Complete: true},
			local:    []LocalDocView{diffFailedView("same")},
			cursor:   cursorOld,
			want:     counts{update: 1},
		},
		// 行: EditTime 未知（零值） => ToUpdate（必须 Fetch 后 hash 判断）
		{
			name:     "EditTime 零值 => ToUpdate",
			snapshot: sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("same", time.Time{})}, Complete: true},
			local:    []LocalDocView{diffLocalView("same")},
			cursor:   cursorOld,
			want:     counts{update: 1},
		},
		// 行: EditTime > cursor => ToUpdate
		{
			name:     "EditTime > cursor => ToUpdate",
			snapshot: sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("same", editNew)}, Complete: true},
			local:    []LocalDocView{diffLocalView("same")},
			cursor:   cursorOld,
			want:     counts{update: 1},
		},
		// 行: EditTime <= cursor 且无重试 => Skipped
		{
			name:     "EditTime <= cursor 且无重试 => Skipped",
			snapshot: sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("same", editOld)}, Complete: true},
			local:    []LocalDocView{diffLocalView("same")},
			cursor:   cursorOld,
			want:     counts{skipped: 1},
		},
		// 行: EditTime == cursor 也算 <= cursor（边界） => Skipped
		{
			name:     "EditTime == cursor => Skipped",
			snapshot: sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("same", cursorOld)}, Complete: true},
			local:    []LocalDocView{diffLocalView("same")},
			cursor:   cursorOld,
			want:     counts{skipped: 1},
		},
		// 组合: 一个新增、一个更新、一个跳过、一个删除
		{
			name: "混合: add + update + skip + remove",
			snapshot: sourceport.TreeSnapshot{
				Nodes: []model.ExternalNode{
					diffDocNode("only-remote", editNew), // add
					diffDocNode("changed", editNew),     // update (>cursor)
					diffDocNode("stable", editOld),      // skip (<=cursor, no retry)
				},
				Complete: true,
			},
			local: []LocalDocView{
				diffLocalView("changed"),
				diffLocalView("stable"),
				diffLocalView("gone"), // 本地有、远端无 => remove
			},
			cursor: cursorOld,
			want:   counts{add: 1, update: 1, skipped: 1, remove: 1},
		},
		// 已软删但远端重新出现 => ToUpdate（spec 5.3）
		{
			name:     "已软删 + 远端重新出现 => ToUpdate",
			snapshot: sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("back", editNew)}, Complete: true},
			local:    []LocalDocView{diffDeletedView("back")},
			cursor:   cursorOld,
			want:     counts{update: 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := diff(tc.snapshot, tc.local, tc.cursor, tc.force)
			got := counts{
				add:      len(plan.ToAdd),
				update:   len(plan.ToUpdate),
				remove:   len(plan.ToRemove),
				skipped:  plan.Skipped,
				warnings: len(plan.Warnings),
			}
			if got != tc.want {
				t.Fatalf("plan 计数不匹配: got=%+v want=%+v (warnings=%v)", got, tc.want, plan.Warnings)
			}
		})
	}
}

// ---- diff: 去重规则（spec 5.1） ----

func TestDiffDedupRemoteDuplicateToken(t *testing.T) {
	// 同一 snapshot 内 remote token 重复：保留首项，丢弃后续，加 warning，重复项不进入删除集合。
	// 这里 local 为空，所以两份重复应只产生 1 个 ToAdd + 1 个 warning。
	// 同时本地故意放一个 external_id 与重复 token 相同的文档，验证重复 token 不会被当作"删除"。
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffDocNode("dup", editNew),
			diffDocNode("dup", editNew), // 重复
		},
		Complete: true,
	}
	local := []LocalDocView{diffLocalView("dup")}

	plan := diff(snapshot, local, time.Time{}, false)

	if len(plan.ToAdd) != 0 {
		t.Errorf("remote 重复 token 与 local 同名时应全部走 update/去重，got ToAdd=%d", len(plan.ToAdd))
	}
	if len(plan.ToUpdate) != 1 {
		t.Fatalf("remote 重复 token 去重后应只保留首项匹配本地，got ToUpdate=%d", len(plan.ToUpdate))
	}
	if plan.ToUpdate[0].Remote.Token != "dup" {
		t.Errorf("ToUpdate 的 remote token 应为 dup，got %q", plan.ToUpdate[0].Remote.Token)
	}
	if len(plan.ToRemove) != 0 {
		t.Errorf("不应有删除，got ToRemove=%d", len(plan.ToRemove))
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("remote 重复 token 应产生 1 条 warning，got %d: %v", len(plan.Warnings), plan.Warnings)
	}
}

func TestDiffDedupRemoteDuplicateWithLocalMissing(t *testing.T) {
	// local 为空、remote 重复：保留首项 ToAdd，丢弃第二项，加 warning。
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffDocNode("dup", editNew),
			diffDocNode("dup", editNew), // 重复
			diffDocNode("ok", editNew),
		},
		Complete: true,
	}
	plan := diff(snapshot, nil, time.Time{}, false)

	if len(plan.ToAdd) != 2 {
		t.Fatalf("去重后应有 2 个 ToAdd（dup 首项 + ok），got %d", len(plan.ToAdd))
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("应有 1 条 warning，got %d", len(plan.Warnings))
	}
}

func TestDiffDedupLocalDuplicateExternalID(t *testing.T) {
	// 本地有两条相同 external_id 的文档：保留首项用于 update，从 ToRemove 排除，加 warning。
	// 远端没有该 token（Complete=true），按删除规则这两条理论上都应被删除，但因为是重复项，
	// 必须排除出删除集合，绝不自动合并业务数据。
	snapshot := sourceport.TreeSnapshot{Nodes: nil, Complete: true}
	local := []LocalDocView{
		diffLocalView("dup"),
		diffLocalView("dup"), // 重复
	}
	plan := diff(snapshot, local, time.Time{}, false)

	if len(plan.ToRemove) != 0 {
		t.Fatalf("本地重复 external_id 必须从 ToRemove 排除，got %d", len(plan.ToRemove))
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("本地重复 external_id 应产生 1 条 warning，got %d: %v", len(plan.Warnings), plan.Warnings)
	}
}

func TestDiffDedupLocalDuplicateKeepsFirstForUpdate(t *testing.T) {
	// 远端有匹配节点，本地重复：首项用于 ToUpdate，重复项产生 warning。
	snapshot := sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("dup", editNew)}, Complete: true}
	local := []LocalDocView{
		diffLocalView("dup"),
		diffLocalView("dup"),
	}
	plan := diff(snapshot, local, cursorOld, false)

	if len(plan.ToUpdate) != 1 {
		t.Fatalf("本地重复 external_id 去重后应只保留 1 个 ToUpdate，got %d", len(plan.ToUpdate))
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("本地重复应产生 1 条 warning，got %d: %v", len(plan.Warnings), plan.Warnings)
	}
}

// ---- diff: folder 节点不进入文档 diff ----

func TestDiffIgnoresNonDocumentNodes(t *testing.T) {
	// folder 节点（HasDocument=false）不应进入文档 diff，也不应触发删除。
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffFolderNode("folder-1", editNew),
			diffDocNode("doc-1", editNew),
		},
		Complete: true,
	}
	local := []LocalDocView{diffLocalView("folder-1")} // 本地以 folder-1 作为 external_id 的文档
	plan := diff(snapshot, local, cursorOld, false)

	if len(plan.ToAdd) != 1 || plan.ToAdd[0].Token != "doc-1" {
		t.Fatalf("folder 不应进入文档 diff，ToAdd 应只有 doc-1，got %+v", plan.ToAdd)
	}
	if len(plan.ToRemove) != 1 {
		// 本地 folder-1 文档在远端文档视图里不存在（folder 节点被忽略），Complete=true => 删除。
		t.Fatalf("本地 folder-1 文档应进入 ToRemove，got %d", len(plan.ToRemove))
	}
	if plan.ToRemove[0].ExternalID != "folder-1" {
		t.Errorf("ToRemove 应为 folder-1，got %q", plan.ToRemove[0].ExternalID)
	}
}

// ---- diff: force 优先级与 RetryRequired 优先级 ----

func TestDiffForceOverridesSkipForAllMatching(t *testing.T) {
	// force 让所有 有/有 节点都进入 ToUpdate，即使本应 skip。
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffDocNode("a", editOld),
			diffDocNode("b", cursorOld),
		},
		Complete: true,
	}
	local := []LocalDocView{diffLocalView("a"), diffLocalView("b")}
	plan := diff(snapshot, local, cursorOld, true)

	if len(plan.ToUpdate) != 2 {
		t.Fatalf("force 应让两个匹配节点都 ToUpdate，got %d", len(plan.ToUpdate))
	}
	if plan.Skipped != 0 {
		t.Errorf("force 下不应有 skip，got %d", plan.Skipped)
	}
}

func TestDiffRetryRequiredBeatsCursor(t *testing.T) {
	// 同一个节点同时满足 RetryRequired=true 和 EditTime<=cursor，RetryRequired 优先 => ToUpdate。
	snapshot := sourceport.TreeSnapshot{Nodes: []model.ExternalNode{diffDocNode("a", editOld)}, Complete: true}
	local := []LocalDocView{diffFailedView("a")}
	plan := diff(snapshot, local, cursorOld, false)

	if len(plan.ToUpdate) != 1 {
		t.Fatalf("RetryRequired 应优先于 cursor，got ToUpdate=%d", len(plan.ToUpdate))
	}
	if plan.Skipped != 0 {
		t.Errorf("RetryRequired 节点不应 skip，got %d", plan.Skipped)
	}
}

// ---- computeSafeCursor 测试（spec 6.4） ----

func TestSafeCursorIncompleteSnapshotReturnsPrevious(t *testing.T) {
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{diffDocNode("a", editNew)},
		// Complete=false
	}
	outcomes := []nodeOutcome{{Token: "a", EditTime: editNew, Result: nodeResultSuccess}}
	got := computeSafeCursor(snapshot, outcomes, cursorOld)
	if !got.Equal(cursorOld) {
		t.Fatalf("incomplete snapshot 应返回 previous，got %v want %v", got, cursorOld)
	}
}

func TestSafeCursorAllSuccessAdvancesToMaxEditTime(t *testing.T) {
	a := diffDocNode("a", editOld)
	b := diffDocNode("b", time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC))
	c := diffDocNode("c", editNew)
	snapshot := sourceport.TreeSnapshot{Nodes: []model.ExternalNode{a, b, c}, Complete: true}
	outcomes := []nodeOutcome{
		{Token: "a", EditTime: editOld, Result: nodeResultSuccess},
		{Token: "b", Result: nodeResultSuccess}, // EditTime 字段可以从 snapshot 拿，这里也填上
		{Token: "c", EditTime: editNew, Result: nodeResultSuccess},
	}
	got := computeSafeCursor(snapshot, outcomes, cursorOld)
	if !got.Equal(editNew) {
		t.Fatalf("全成功应推进到最大 EditTime，got %v want %v", got, editNew)
	}
}

func TestSafeCursorStopsAtFirstFailure(t *testing.T) {
	a := diffDocNode("a", editOld)
	b := diffDocNode("b", time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC))
	c := diffDocNode("c", editNew)
	snapshot := sourceport.TreeSnapshot{Nodes: []model.ExternalNode{a, b, c}, Complete: true}
	outcomes := []nodeOutcome{
		{Token: "a", EditTime: editOld, Result: nodeResultSuccess},
		{Token: "b", Result: nodeResultFailure}, // 失败，断开前缀
		{Token: "c", EditTime: editNew, Result: nodeResultSuccess},
	}
	// previous 为零值，模拟一次全新/重置的同步，避免触发"watermark 不得倒退"规则。
	got := computeSafeCursor(snapshot, outcomes, time.Time{})
	want := editOld
	if !got.Equal(want) {
		t.Fatalf("应在第一个失败处停止，watermark 为最后成功时间 %v，got %v", want, got)
	}
}

func TestSafeCursorSameEditTimeGroupPartialFailure(t *testing.T) {
	// 同一 EditTime 的两个节点，一个成功一个失败 => 不能推进到该时间。
	t1 := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffDocNode("a", editOld),
			diffDocNode("b1", t1),
			diffDocNode("b2", t1),
		},
		Complete: true,
	}
	outcomes := []nodeOutcome{
		{Token: "a", EditTime: editOld, Result: nodeResultSuccess},
		{Token: "b1", EditTime: t1, Result: nodeResultSuccess},
		{Token: "b2", EditTime: t1, Result: nodeResultFailure},
	}
	// previous 为零值，模拟全新同步。
	got := computeSafeCursor(snapshot, outcomes, time.Time{})
	if !got.Equal(editOld) {
		t.Fatalf("同 EditTime 组部分失败应回退到上一组时间 %v，got %v", editOld, got)
	}
}

func TestSafeCursorSameEditTimeGroupAllSuccess(t *testing.T) {
	t1 := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffDocNode("a", editOld),
			diffDocNode("b1", t1),
			diffDocNode("b2", t1),
		},
		Complete: true,
	}
	outcomes := []nodeOutcome{
		{Token: "a", EditTime: editOld, Result: nodeResultSuccess},
		{Token: "b1", EditTime: t1, Result: nodeResultSuccess},
		{Token: "b2", EditTime: t1, Result: nodeResultSuccess},
	}
	got := computeSafeCursor(snapshot, outcomes, cursorOld)
	if !got.Equal(t1) {
		t.Fatalf("同 EditTime 组全部成功应推进到该时间 %v，got %v", t1, got)
	}
}

func TestSafeCursorZeroEditTimeIgnored(t *testing.T) {
	// EditTime 零值节点不参与 watermark 推进，应被跳过；不影响后续成功前缀。
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffDocNode("zero", time.Time{}), // 零值，跳过
			diffDocNode("a", editOld),
		},
		Complete: true,
	}
	outcomes := []nodeOutcome{
		{Token: "zero", EditTime: time.Time{}, Result: nodeResultSuccess},
		{Token: "a", EditTime: editOld, Result: nodeResultSuccess},
	}
	// previous 为零值，模拟全新同步。
	got := computeSafeCursor(snapshot, outcomes, time.Time{})
	if !got.Equal(editOld) {
		t.Fatalf("零 EditTime 节点应被跳过，watermark 应推进到 %v，got %v", editOld, got)
	}
}

func TestSafeCursorZeroEditTimeDoesNotBreakPrefix(t *testing.T) {
	// 零值节点不参与前缀判定，不应阻断后续成功节点。
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffDocNode("a", editOld),
			diffDocNode("zero", time.Time{}), // 零值
			diffDocNode("b", editNew),
		},
		Complete: true,
	}
	outcomes := []nodeOutcome{
		{Token: "a", EditTime: editOld, Result: nodeResultSuccess},
		{Token: "zero", EditTime: time.Time{}, Result: nodeResultSuccess},
		{Token: "b", EditTime: editNew, Result: nodeResultSuccess},
	}
	got := computeSafeCursor(snapshot, outcomes, cursorOld)
	if !got.Equal(editNew) {
		t.Fatalf("零值节点不应阻断前缀，watermark 应推进到 %v，got %v", editNew, got)
	}
}

func TestSafeCursorMissingOutcomeStopsPrefix(t *testing.T) {
	// 某节点没有对应 outcome => 视为未知，前缀在此停止。
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffDocNode("a", editOld),
			diffDocNode("b", editNew), // 缺少 outcome
		},
		Complete: true,
	}
	outcomes := []nodeOutcome{
		{Token: "a", EditTime: editOld, Result: nodeResultSuccess},
		// b 没有 outcome
	}
	// previous 为零值，模拟全新同步。
	got := computeSafeCursor(snapshot, outcomes, time.Time{})
	if !got.Equal(editOld) {
		t.Fatalf("缺少 outcome 应断开前缀，watermark 应为 %v，got %v", editOld, got)
	}
}

func TestSafeCursorNoDocumentNodesReturnsPrevious(t *testing.T) {
	// 只有 folder 节点（非文档），没有可推进的节点 => 返回 previous。
	snapshot := sourceport.TreeSnapshot{
		Nodes:    []model.ExternalNode{diffFolderNode("f", editNew)},
		Complete: true,
	}
	got := computeSafeCursor(snapshot, nil, cursorOld)
	if !got.Equal(cursorOld) {
		t.Fatalf("无可推进节点应返回 previous %v，got %v", cursorOld, got)
	}
}

func TestSafeCursorAllFailureReturnsPrevious(t *testing.T) {
	snapshot := sourceport.TreeSnapshot{
		Nodes:    []model.ExternalNode{diffDocNode("a", editOld)},
		Complete: true,
	}
	outcomes := []nodeOutcome{{Token: "a", EditTime: editOld, Result: nodeResultFailure}}
	got := computeSafeCursor(snapshot, outcomes, cursorOld)
	if !got.Equal(cursorOld) {
		t.Fatalf("全部失败应返回 previous %v，got %v", cursorOld, got)
	}
}

func TestSafeCursorPreviousHigherThanComputed(t *testing.T) {
	// 如果计算出的 watermark 比 previous 还旧，不应回退 previous。
	future := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	snapshot := sourceport.TreeSnapshot{
		Nodes:    []model.ExternalNode{diffDocNode("a", editOld)},
		Complete: true,
	}
	outcomes := []nodeOutcome{{Token: "a", EditTime: editOld, Result: nodeResultSuccess}}
	got := computeSafeCursor(snapshot, outcomes, future)
	if !got.Equal(future) {
		t.Fatalf("不应回退 previous，got %v want %v", got, future)
	}
}

// TestSafeCursorSkippedNodesAdvanceOverCursor 验证 spec 6.4：
// 被 cursor 覆盖的 skip 节点（EditTime <= previous）必须以成功 outcome 参与 watermark，
// 否则缺少 outcome 会断开前缀，导致更晚的成功节点无法把 watermark 推过 cursor。
func TestSafeCursorSkippedNodesAdvanceOverCursor(t *testing.T) {
	// previous=cursorOld(6/1)；a 的 EditTime=editOld(5/1) <= previous（被 skip）；
	// b 的 EditTime=editNew(7/1) > previous（本轮成功）。
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffDocNode("a", editOld),
			diffDocNode("b", editNew),
		},
		Complete: true,
	}
	// 调用方（applyDocumentNodes）为 skip 节点补发成功 outcome。
	outcomes := []nodeOutcome{
		{Token: "a", EditTime: editOld, Result: nodeResultSuccess}, // skipped but success
		{Token: "b", EditTime: editNew, Result: nodeResultSuccess},
	}
	got := computeSafeCursor(snapshot, outcomes, cursorOld)
	if !got.Equal(editNew) {
		t.Fatalf("skip 节点应允许 watermark 推过 cursor，got %v want %v", got, editNew)
	}
}

// TestDiffPopulatesSkippedNodes 验证 diff 把被 cursor 跳过的节点放入 plan.SkippedNodes，
// 让调用方可以为它们补发成功 outcome（配合 computeSafeCursor 的前缀推进）。
func TestDiffPopulatesSkippedNodes(t *testing.T) {
	snapshot := sourceport.TreeSnapshot{
		Nodes: []model.ExternalNode{
			diffDocNode("skip-me", editOld),
			diffDocNode("new-me", editNew),
		},
		Complete: true,
	}
	// skip-me 本地已存在、内容未变、EditTime(5/1) <= cursor(6/1) => skipped。
	// new-me 本地不存在 => ToAdd。
	local := []LocalDocView{diffLocalView("skip-me")}
	plan := diff(snapshot, local, cursorOld, false)
	if plan.Skipped != 1 || len(plan.SkippedNodes) != 1 {
		t.Fatalf("expect 1 skipped node recorded, got Skipped=%d SkippedNodes=%d",
			plan.Skipped, len(plan.SkippedNodes))
	}
	if plan.SkippedNodes[0].Token != "skip-me" {
		t.Fatalf("skipped node token = %q, want skip-me", plan.SkippedNodes[0].Token)
	}
	if len(plan.ToAdd) != 1 || plan.ToAdd[0].Token != "new-me" {
		t.Fatalf("expect new-me in ToAdd, got %#v", plan.ToAdd)
	}
}
