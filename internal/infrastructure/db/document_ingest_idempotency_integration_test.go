//go:build integration

package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"

	appservice "github.com/dajee/langhuan/internal/application/service"
)

// 回归 LH-001 缺陷 B1：document_ingest_idempotencies 主键曾恒为零值 UUID
// （service 结构刻意不携带 ID，Row 转换也未生成），第一条记录占位后所有
// 后续幂等记录永远撞主键。本测试在两种方言上验证：连续创建多条幂等记录、
// 主键各不相同、按自然键重放各自血缘、缺失键返回 ErrNotFound。
func TestDocumentIngestIdempotencyConsecutiveRecords(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		ctx, database := newAuthTestDB(t)
		seed := insertKnowledgeSchemaSeed(t, ctx, database)
		runDocumentIngestIdempotencyFlow(t, ctx, database, seed.workspaceID, seed.kbID, seed.rootID)
	})
	t.Run("sqlite", func(t *testing.T) {
		database := openSQLiteTestDB(t)
		ctx := context.Background()
		wsID := uuid.New()
		now := time.Now().UTC()
		if err := database.WithContext(ctx).Exec(
			// SQLite 腿走 openSQLiteTestDB 的 FK-off 连接，仅插幂等流程必需行。
			"INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
			wsID, "idem", "idem-"+wsID.String()[:8], now, now,
		).Error; err != nil {
			t.Fatalf("插入 workspace 失败: %v", err)
		}
		kbID := uuid.New()
		rootID := uuid.New()
		runDocumentIngestIdempotencyFlow(t, ctx, database, wsID, kbID, rootID)
	})
}

func runDocumentIngestIdempotencyFlow(
	t *testing.T, ctx context.Context, database *gorm.DB, workspaceID, kbID, rootID uuid.UUID,
) {
	t.Helper()
	apiKeyID := insertIdempotencyAPIKey(t, ctx, database, workspaceID)

	type lineage struct {
		key      string
		sha      string
		document *model.Document
		job      *model.Job
	}
	records := []lineage{
		{key: "idem-key-1", sha: strings.Repeat("a", 64)},
		{key: "idem-key-2", sha: strings.Repeat("b", 64)},
	}

	store := NewDocumentIngestDBStore(database)
	for i := range records {
		item := &records[i]
		document, revision, node, job := newFileIngestAggregate(
			t, workspaceID, kbID, rootID, "idem-"+item.key+".md",
		)
		if err := store.WithinWorkspace(
			ctx, workspaceID, func(txCtx context.Context, tx appservice.DocumentIngestTx) error {
				if err := tx.CreateFileDocumentNodeRevisionAndJob(txCtx, document, node, revision, job); err != nil {
					return err
				}
				return tx.CreateIdempotencyRecord(txCtx, appservice.DocumentIngestIdempotency{
					WorkspaceID:     workspaceID,
					APIKeyID:        apiKeyID,
					KnowledgeBaseID: kbID,
					Key:             item.key,
					RequestSHA256:   item.sha,
					DocumentID:      document.ID,
					JobID:           job.ID,
				})
			},
		); err != nil {
			t.Fatalf("创建第 %d 条幂等记录失败（B1 回归点）: %v", i+1, err)
		}
		item.document, item.job = document, job
	}

	var rows []DocumentIngestIdempotencyRow
	if err := database.WithContext(ctx).
		Where("workspace_id = ? AND api_key_id = ?", workspaceID, apiKeyID).
		Order("key").
		Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(records) {
		t.Fatalf("幂等记录数 = %d, want %d", len(rows), len(records))
	}
	if rows[0].ID == rows[1].ID || rows[0].ID == (uuid.UUID{}) || rows[1].ID == (uuid.UUID{}) {
		t.Fatalf("幂等记录主键异常（B1 症状）: %s / %s", rows[0].ID, rows[1].ID)
	}

	for _, item := range records {
		replay, err := store.ReplayIdempotentIngest(ctx, workspaceID, apiKeyID, kbID, item.key)
		if err != nil {
			t.Fatalf("重放 %s 失败: %v", item.key, err)
		}
		if replay.Record.RequestSHA256 != item.sha || replay.Record.DocumentID != item.document.ID {
			t.Fatalf(
				"重放 %s 血缘不符: sha=%s doc=%s",
				item.key, replay.Record.RequestSHA256, replay.Record.DocumentID,
			)
		}
	}
	if _, err := store.ReplayIdempotentIngest(
		ctx, workspaceID, apiKeyID, kbID, "idem-key-missing",
	); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("缺失幂等键应返回 ErrNotFound, got %v", err)
	}
}

func insertIdempotencyAPIKey(
	t *testing.T, ctx context.Context, database *gorm.DB, workspaceID uuid.UUID,
) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte(uuid.NewString()))
	row := &WorkspaceAPIKeyRow{
		ID:                    uuid.New(),
		WorkspaceID:           workspaceID,
		Name:                  "idem-key",
		TokenHash:             hex.EncodeToString(hash[:]),
		TokenPrefix:           "lhk_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8],
		TokenSecretCiphertext: []byte("dummy-cipher-data-0123456789"),
		Scopes:                []string{"documents:write"},
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := database.WithContext(ctx).Create(row).Error; err != nil {
		t.Fatalf("插入 API Key 失败: %v", err)
	}
	return row.ID
}
