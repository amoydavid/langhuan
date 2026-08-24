//go:build integration

package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	"github.com/dajee/langhuan/internal/infrastructure/migrate"
)

// TestSQLiteWorkspaceTxDefersForwardForeignKey 验证 SQLite Workspace 事务开启
// defer_foreign_keys：knowledge_bases 的前向复合 FK（file_tree_root_id、
// active_index_generation_id）在 PG 中为 DEFERRABLE，KB 创建事务按
// kb 行 -> root -> generation 顺序插入；SQLite 若不延迟检查会立即违反
// 前向 FK（standalone 模式创建知识库 500 的根因）。
func TestSQLiteWorkspaceTxDefersForwardForeignKey(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "tx-fk.db") + "?cache=shared"
	cfg := config.DatabaseConfig{Driver: "sqlite", DSN: dsn, AutoMigrate: true}
	if err := migrate.Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	gormDB, _, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer func() { c, _ := gormDB.DB(); _ = c.Close() }()

	workspaceID := "00000000-0000-0000-0000-000000000010"
	if err := gormDB.Exec(
		"INSERT INTO workspaces (id, name, slug) VALUES (?, ?, ?)", workspaceID, "ws", "ws-tx-fk",
	).Error; err != nil {
		t.Fatalf("插入 workspace 失败: %v", err)
	}
	providerID := "00000000-0000-0000-0000-000000000020"
	modelID := "00000000-0000-0000-0000-000000000021"
	if err := gormDB.Exec(
		"INSERT INTO model_providers (id, scope, name, provider) VALUES (?, 'platform', 'p', 'openai')", providerID,
	).Error; err != nil {
		t.Fatalf("插入 provider 失败: %v", err)
	}
	if err := gormDB.Exec(
		"INSERT INTO models (id, provider_id, name, type, model_name, dimensions) VALUES (?, ?, 'm', 'embedding', 'mock', 1024)",
		modelID, providerID,
	).Error; err != nil {
		t.Fatalf("插入 model 失败: %v", err)
	}

	kbID := uuid.New().String()
	rootID := uuid.New().String()
	generationID := uuid.New().String()
	runner := db.NewWorkspaceTxRunner(gormDB)
	err = runner.WithinWorkspace(context.Background(), uuid.MustParse(workspaceID), func(tx *gorm.DB) error {
		// 与 knowledge_base_creation_store 相同的插入顺序：kb 行先于其前向引用的
		// root 与 generation。修复前该顺序在 foreign_keys=ON 的 SQLite 上立即失败。
		if err := tx.Exec(
			"INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id) VALUES (?, ?, 'kb', ?)",
			kbID, workspaceID, rootID,
		).Error; err != nil {
			return err
		}
		if err := tx.Exec(
			"INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES (?, ?, ?, 'root', '')",
			rootID, workspaceID, kbID,
		).Error; err != nil {
			return err
		}
		return tx.Exec(
			"INSERT INTO knowledge_base_index_generations (id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, embedding_dimension, model_config_hash, chunker_version, config_hash, status) "+
				"VALUES (?, ?, ?, ?, ?, 'mock', 1024, 'mhash', 3, 'chash', 'ready')",
			generationID, workspaceID, kbID, modelID, providerID,
		).Error
	})
	if err != nil {
		t.Fatalf("KB 创建事务失败（前向 FK 未被延迟）: %v", err)
	}

	var count int64
	if err := gormDB.Table("knowledge_bases").Where("id = ?", kbID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("kb 行未落库: count=%d err=%v", count, err)
	}
}
