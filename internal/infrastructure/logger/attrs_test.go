package logger

import (
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

func TestLineageAttrsSkipsZeroUUIDs(t *testing.T) {
	ws := uuid.New()
	attrs := LineageAttrs(ws, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil)
	// 每个 slog.String(...) 追加一个 slog.Attr 元素；只 workspace_id 非空 → 1 个。
	if len(attrs) != 1 {
		t.Fatalf("attrs len = %d, want 1 (only workspace_id)", len(attrs))
	}
	attr, ok := attrs[0].(slog.Attr)
	if !ok || attr.Key != FieldWorkspaceID {
		t.Fatalf("first attr = %#v, want key %s", attrs[0], FieldWorkspaceID)
	}
}

func TestLineageAttrsIncludesAllNonZero(t *testing.T) {
	ws, kb, doc, rev, job := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	attrs := LineageAttrs(ws, kb, doc, rev, job)
	// 5 个非零字段 → 5 个 slog.Attr 元素。
	if len(attrs) != 5 {
		t.Fatalf("attrs len = %d, want 5", len(attrs))
	}
	keys := make(map[string]bool, 5)
	for _, a := range attrs {
		if attr, ok := a.(slog.Attr); ok {
			keys[attr.Key] = true
		}
	}
	for _, want := range []string{FieldWorkspaceID, FieldKnowledgeBaseID, FieldDocumentID, FieldRevisionID, FieldJobID} {
		if !keys[want] {
			t.Fatalf("missing field %s in lineage attrs", want)
		}
	}
}

func TestStageAttrsShape(t *testing.T) {
	attrs := StageAttrs("index", "ok", 42)
	pairs := make([]slog.Attr, 0, len(attrs)/2)
	// StageAttrs 返回 []any 形式的 slog.String 调用结果。
	for _, a := range attrs {
		pairs = append(pairs, a.(slog.Attr))
	}
	if len(pairs) != 4 {
		t.Fatalf("attr pairs = %d, want 4", len(pairs))
	}
	got := map[string]string{}
	for _, p := range pairs {
		got[p.Key] = p.Value.String()
	}
	if got[FieldEvent] != "pipeline.stage.completed" || got[FieldStage] != "index" ||
		got[FieldStatus] != "ok" || got[FieldDurationMS] != "42" {
		t.Fatalf("stage attrs = %#v", got)
	}
}
