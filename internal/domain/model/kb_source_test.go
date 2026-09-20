package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// TestNewKbSourceValidationMatrix 覆盖 NewKbSource 的必填/枚举校验矩阵。
func TestNewKbSourceValidationMatrix(t *testing.T) {
	workspaceID, kbID := uuid.New(), uuid.New()
	tests := []struct {
		name  string
		input NewKbSourceInput
	}{
		{
			name: "workspace 为空",
			input: NewKbSourceInput{
				KnowledgeBaseID: kbID, Kind: value.KbSourceKindWeb, Name: "产品文档站",
			},
		},
		{
			name: "kb 为空",
			input: NewKbSourceInput{
				WorkspaceID: workspaceID, Kind: value.KbSourceKindWeb, Name: "产品文档站",
			},
		},
		{
			name: "kind 为空",
			input: NewKbSourceInput{
				WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Name: "产品文档站",
			},
		},
		{
			name: "kind 非 web",
			input: NewKbSourceInput{
				WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Kind: "rss", Name: "产品文档站",
			},
		},
		{
			name: "name 为空",
			input: NewKbSourceInput{
				WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Kind: value.KbSourceKindWeb,
			},
		},
		{
			name: "name 全空白",
			input: NewKbSourceInput{
				WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Kind: value.KbSourceKindWeb, Name: "   \t ",
			},
		},
		{
			name: "status 非枚举值",
			input: NewKbSourceInput{
				WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Kind: value.KbSourceKindWeb,
				Name: "产品文档站", Status: "paused",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewKbSource(tt.input)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if source != nil {
				t.Fatalf("source = %+v, want nil on validation error", source)
			}
		})
	}
}

// TestNewKbSourceDefaults 固定合法输入的缺省值：status=active、config 非 nil、
// 运行态列取建表默认（空 map / 未设 next_sync_at / force latch 关闭）。
func TestNewKbSourceDefaults(t *testing.T) {
	workspaceID, kbID := uuid.New(), uuid.New()
	source, err := NewKbSource(NewKbSourceInput{
		WorkspaceID:     workspaceID,
		KnowledgeBaseID: kbID,
		Kind:            value.KbSourceKindWeb,
		Name:            "  产品文档站  ",
	})
	if err != nil {
		t.Fatalf("NewKbSource err = %v", err)
	}
	if source.ID == uuid.Nil {
		t.Fatal("ID 不应为零值")
	}
	if source.WorkspaceID != workspaceID || source.KnowledgeBaseID != kbID {
		t.Fatalf("lineage = %s/%s, want %s/%s", source.WorkspaceID, source.KnowledgeBaseID, workspaceID, kbID)
	}
	if source.Kind != value.KbSourceKindWeb {
		t.Fatalf("kind = %q, want web", source.Kind)
	}
	if source.Name != "产品文档站" {
		t.Fatalf("name = %q, want trimmed", source.Name)
	}
	if source.Status != value.KbSourceStatusActive {
		t.Fatalf("status = %q, want active", source.Status)
	}
	if source.Config == nil || source.SyncCursor == nil || source.SyncLastResult == nil {
		t.Fatalf("config/cursor/last_result 应为空 map 而非 nil: %+v", source)
	}
	if source.NextSyncAt != nil {
		t.Fatalf("next_sync_at = %v, want nil", source.NextSyncAt)
	}
	if source.SyncRequestedForce {
		t.Fatal("sync_requested_force 默认应为 false")
	}
	if source.DeletedAt != nil {
		t.Fatalf("deleted_at = %v, want nil", source.DeletedAt)
	}
	if source.CreatedAt.IsZero() || source.UpdatedAt.IsZero() {
		t.Fatal("created_at/updated_at 应被填充")
	}
}

// TestNewKbSourceKeepsProvidedConfigAndStatus 验证调用方传入的 config 与显式
// status 被原样承载（config 的强类型解析是 application 层职责，见 §4.1）。
func TestNewKbSourceKeepsProvidedConfigAndStatus(t *testing.T) {
	config := map[string]any{"mode": "urls", "urls": []any{"https://example.com/a"}}
	source, err := NewKbSource(NewKbSourceInput{
		WorkspaceID:     uuid.New(),
		KnowledgeBaseID: uuid.New(),
		Kind:            value.KbSourceKindWeb,
		Name:            "URL 列表源",
		Status:          value.KbSourceStatusDisabled,
		Config:          config,
	})
	if err != nil {
		t.Fatalf("NewKbSource err = %v", err)
	}
	if source.Status != value.KbSourceStatusDisabled {
		t.Fatalf("status = %q, want disabled", source.Status)
	}
	if got := source.Config["mode"]; got != "urls" {
		t.Fatalf("config.mode = %v, want urls", got)
	}
}
