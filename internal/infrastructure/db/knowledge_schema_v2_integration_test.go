//go:build integration

package db

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestKnowledgeSchemaV2Tables(t *testing.T) {
	ctx, database := openIntegrationTestDB(t)
	for _, table := range []string{
		"knowledge_base_index_generations",
		"document_revisions",
		"faq_revision_contents",
		"faq_revision_questions",
		"document_chunk_sets",
		"chunk_revisions",
		"file_tree_nodes",
		"retrieval_entries",
	} {
		var exists bool
		if err := database.WithContext(ctx).Raw(
			"SELECT to_regclass(?) IS NOT NULL",
			table,
		).Scan(&exists).Error; err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s does not exist", table)
		}
	}
}

func TestKnowledgeSchemaV2GenerationHasNoFailedCount(t *testing.T) {
	ctx, database := openIntegrationTestDB(t)
	var exists bool
	if err := database.WithContext(ctx).Raw(
		"SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'knowledge_base_index_generations' AND column_name = 'failed_count')",
	).Scan(&exists).Error; err != nil {
		t.Fatalf("check failed_count column: %v", err)
	}
	if exists {
		t.Fatal("knowledge_base_index_generations.failed_count still exists")
	}
}

type knowledgeSchemaSeed struct {
	workspaceID  uuid.UUID
	userID       uuid.UUID
	providerID   uuid.UUID
	modelID      uuid.UUID
	kbID         uuid.UUID
	rootID       uuid.UUID
	generationID uuid.UUID
}

func TestKnowledgeSchemaV2RejectsCrossTenantDocument(t *testing.T) {
	ctx, database := openIntegrationTestDB(t)
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed := insertKnowledgeSchemaSeed(t, ctx, tx)
		otherWorkspaceID := uuid.New()
		if err := tx.Exec(
			"INSERT INTO workspaces (id, name, slug) VALUES (?, 'other', ?)",
			otherWorkspaceID, "other-"+uuid.NewString(),
		).Error; err != nil {
			return err
		}
		return tx.Exec(
			"INSERT INTO documents "+
				"(id, workspace_id, knowledge_base_id, kind, title, source_type, source_uri, status) "+
				"VALUES (?, ?, ?, 'web', 'cross tenant', 'crawler', 'https://example.com/', 'pending')",
			uuid.New(), otherWorkspaceID, seed.kbID,
		).Error
	})
	if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatalf("cross tenant error = %v, want foreign key violation", err)
	}
}

func TestKnowledgeSchemaV2ConstraintMatrix(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(context.Context, *gorm.DB, knowledgeSchemaSeed) error
		want   error
	}{
		{
			name: "Document and revision kinds must match",
			mutate: func(ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed) error {
				documentID := uuid.New()
				if err := tx.WithContext(ctx).Exec(
					"INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, status) "+
						"VALUES (?, ?, ?, 'faq', 'FAQ', 'api', 'pending')",
					documentID, seed.workspaceID, seed.kbID,
				).Error; err != nil {
					return err
				}
				return tx.WithContext(ctx).Exec(
					"INSERT INTO document_revisions "+
						"(id, workspace_id, knowledge_base_id, document_id, kind, revision_no, revision_reason, "+
						"original_filename, file_type, raw_storage_key, processing_version, status) "+
						"VALUES (?, ?, ?, ?, 'file', 1, 'ingest', 'wrong.md', 'markdown', 'raw/wrong', 1, 'pending')",
					uuid.New(), seed.workspaceID, seed.kbID, documentID,
				).Error
			},
			want: gorm.ErrForeignKeyViolated,
		},
		{
			name: "FAQ revision must have a question",
			mutate: func(ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed) error {
				documentID, revisionID := uuid.New(), uuid.New()
				if err := tx.WithContext(ctx).Exec(
					"INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, status) "+
						"VALUES (?, ?, ?, 'faq', 'FAQ', 'api', 'pending')",
					documentID, seed.workspaceID, seed.kbID,
				).Error; err != nil {
					return err
				}
				if err := insertFAQRevision(ctx, tx, seed, documentID, revisionID); err != nil {
					return err
				}
				return tx.WithContext(ctx).Exec(
					"INSERT INTO faq_revision_contents "+
						"(document_revision_id, workspace_id, knowledge_base_id, document_id, answer) "+
						"VALUES (?, ?, ?, ?, 'answer')",
					revisionID, seed.workspaceID, seed.kbID, documentID,
				).Error
			},
		},
		{
			name: "FAQ content cannot reference File revision",
			mutate: func(ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed) error {
				documentID, revisionID := uuid.New(), uuid.New()
				if err := insertFileDocumentRevision(ctx, tx, seed, documentID, revisionID, "file.md"); err != nil {
					return err
				}
				return tx.WithContext(ctx).Exec(
					"INSERT INTO faq_revision_contents "+
						"(document_revision_id, workspace_id, knowledge_base_id, document_id, answer) "+
						"VALUES (?, ?, ?, ?, 'answer')",
					revisionID, seed.workspaceID, seed.kbID, documentID,
				).Error
			},
			want: gorm.ErrForeignKeyViolated,
		},
		{
			name: "file node cannot reference FAQ document",
			mutate: func(ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed) error {
				documentID := uuid.New()
				if err := tx.WithContext(ctx).Exec(
					"INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, status) "+
						"VALUES (?, ?, ?, 'faq', 'FAQ', 'api', 'pending')",
					documentID, seed.workspaceID, seed.kbID,
				).Error; err != nil {
					return err
				}
				return tx.WithContext(ctx).Exec(
					"INSERT INTO file_tree_nodes "+
						"(id, workspace_id, knowledge_base_id, parent_id, node_type, name, document_id, document_kind) "+
						"VALUES (?, ?, ?, ?, 'file', 'faq.md', ?, 'file')",
					uuid.New(), seed.workspaceID, seed.kbID, seed.rootID, documentID,
				).Error
			},
			want: gorm.ErrForeignKeyViolated,
		},
		{
			name: "parent must belong to same KB",
			mutate: func(ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed) error {
				otherKBID, _, err := insertAdditionalKnowledgeBase(ctx, tx, seed)
				if err != nil {
					return err
				}
				return tx.WithContext(ctx).Exec(
					"INSERT INTO file_tree_nodes "+
						"(id, workspace_id, knowledge_base_id, parent_id, node_type, name) "+
						"VALUES (?, ?, ?, ?, 'folder', 'cross-kb')",
					uuid.New(), seed.workspaceID, otherKBID, seed.rootID,
				).Error
			},
			want: gorm.ErrForeignKeyViolated,
		},
		{
			name: "only one root per KB",
			mutate: func(ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed) error {
				return tx.WithContext(ctx).Exec(
					"INSERT INTO file_tree_nodes "+
						"(id, workspace_id, knowledge_base_id, node_type, name) VALUES (?, ?, ?, 'root', '')",
					uuid.New(), seed.workspaceID, seed.kbID,
				).Error
			},
			want: gorm.ErrDuplicatedKey,
		},
		{
			name: "File document has only one node",
			mutate: func(ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed) error {
				documentID, revisionID := uuid.New(), uuid.New()
				if err := insertFileDocumentRevision(ctx, tx, seed, documentID, revisionID, "unique.md"); err != nil {
					return err
				}
				return tx.WithContext(ctx).Exec(
					"INSERT INTO file_tree_nodes "+
						"(id, workspace_id, knowledge_base_id, parent_id, node_type, name, document_id, document_kind) "+
						"VALUES (?, ?, ?, ?, 'file', 'second.md', ?, 'file')",
					uuid.New(), seed.workspaceID, seed.kbID, seed.rootID, documentID,
				).Error
			},
			want: gorm.ErrDuplicatedKey,
		},
		{
			name: "folder and file share case insensitive namespace",
			mutate: func(ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed) error {
				if err := tx.WithContext(ctx).Exec(
					"INSERT INTO file_tree_nodes "+
						"(id, workspace_id, knowledge_base_id, parent_id, node_type, name) "+
						"VALUES (?, ?, ?, ?, 'folder', 'Docs')",
					uuid.New(), seed.workspaceID, seed.kbID, seed.rootID,
				).Error; err != nil {
					return err
				}
				documentID, revisionID := uuid.New(), uuid.New()
				return insertFileDocumentRevision(ctx, tx, seed, documentID, revisionID, "docs")
			},
			want: gorm.ErrDuplicatedKey,
		},
		{
			name: "active Web URL is unique",
			mutate: func(ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed) error {
				for range 2 {
					if err := tx.WithContext(ctx).Exec(
						"INSERT INTO documents "+
							"(id, workspace_id, knowledge_base_id, kind, title, source_type, source_uri, status) "+
							"VALUES (?, ?, ?, 'web', 'Web', 'crawler', 'https://example.com/', 'pending')",
						uuid.New(), seed.workspaceID, seed.kbID,
					).Error; err != nil {
						return err
					}
				}
				return nil
			},
			want: gorm.ErrDuplicatedKey,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, database := openIntegrationTestDB(t)
			err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				seed := insertKnowledgeSchemaSeed(t, ctx, tx)
				return test.mutate(ctx, tx, seed)
			})
			if err == nil {
				t.Fatal("constraint mutation unexpectedly committed")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func insertAdditionalKnowledgeBase(
	ctx context.Context,
	tx *gorm.DB,
	seed knowledgeSchemaSeed,
) (uuid.UUID, uuid.UUID, error) {
	kbID, rootID, generationID := uuid.New(), uuid.New(), uuid.New()
	if err := tx.WithContext(ctx).Exec(
		"INSERT INTO knowledge_bases (id, workspace_id, name, active_index_generation_id, file_tree_root_id) "+
			"VALUES (?, ?, ?, ?, ?)",
		kbID, seed.workspaceID, "kb-"+uuid.NewString(), generationID, rootID,
	).Error; err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := tx.WithContext(ctx).Exec(
		"INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) "+
			"VALUES (?, ?, ?, 'root', '')",
		rootID, seed.workspaceID, kbID,
	).Error; err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := tx.WithContext(ctx).Exec(
		"INSERT INTO knowledge_base_index_generations "+
			"(id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, "+
			"embedding_dimension, model_config_hash, chunker_version, config_hash, status) "+
			"VALUES (?, ?, ?, ?, ?, 'text-embedding', 1024, 'model-hash', 1, 'config-hash', 'ready')",
		generationID, seed.workspaceID, kbID, seed.modelID, seed.providerID,
	).Error; err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return kbID, rootID, nil
}

func insertKnowledgeSchemaSeed(t *testing.T, ctx context.Context, tx *gorm.DB) knowledgeSchemaSeed {
	t.Helper()
	seed := knowledgeSchemaSeed{
		workspaceID: uuid.New(), userID: uuid.New(), providerID: uuid.New(),
		modelID: uuid.New(), kbID: uuid.New(), rootID: uuid.New(), generationID: uuid.New(),
	}
	statements := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO workspaces (id, name, slug) VALUES (?, 'schema-v2', ?)", []any{seed.workspaceID, "schema-v2-" + uuid.NewString()}},
		{"INSERT INTO users (id, email, nickname, password_hash) VALUES (?, ?, 'schema-v2', 'hash')", []any{seed.userID, uuid.NewString() + "@example.com"}},
		{"INSERT INTO model_providers (id, scope, workspace_id, name, provider, status, created_by) " +
			"VALUES (?, 'workspace', ?, ?, 'openai', 'active', ?)",
			[]any{seed.providerID, seed.workspaceID, "provider-" + uuid.NewString(), seed.userID}},
		{"INSERT INTO models (id, provider_id, name, type, model_name, dimensions, status, created_by) " +
			"VALUES (?, ?, ?, 'embedding', 'text-embedding', 1024, 'active', ?)",
			[]any{seed.modelID, seed.providerID, "model-" + uuid.NewString(), seed.userID}},
		{"INSERT INTO knowledge_bases (id, workspace_id, name, active_index_generation_id, file_tree_root_id) " +
			"VALUES (?, ?, ?, ?, ?)",
			[]any{seed.kbID, seed.workspaceID, "kb-" + uuid.NewString(), seed.generationID, seed.rootID}},
		{"INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) " +
			"VALUES (?, ?, ?, 'root', '')",
			[]any{seed.rootID, seed.workspaceID, seed.kbID}},
		{"INSERT INTO knowledge_base_index_generations " +
			"(id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, " +
			"embedding_dimension, model_config_hash, chunker_version, chunking_config, config_hash, status) " +
			"VALUES (?, ?, ?, ?, ?, 'text-embedding', 1024, 'model-hash', 1, " +
			"'{\"chunk_size\":512,\"chunk_overlap\":80}'::jsonb, 'config-hash', 'ready')",
			[]any{seed.generationID, seed.workspaceID, seed.kbID, seed.modelID, seed.providerID}},
	}
	for _, statement := range statements {
		if err := tx.WithContext(ctx).Exec(statement.sql, statement.args...).Error; err != nil {
			t.Fatalf("seed knowledge schema: %v", err)
		}
	}
	return seed
}

func insertFAQRevision(ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed, documentID, revisionID uuid.UUID) error {
	return tx.WithContext(ctx).Exec(
		"INSERT INTO document_revisions "+
			"(id, workspace_id, knowledge_base_id, document_id, kind, revision_no, revision_reason, "+
			"processing_version, status) "+
			"VALUES (?, ?, ?, ?, 'faq', 1, 'ingest', 1, 'pending')",
		revisionID, seed.workspaceID, seed.kbID, documentID,
	).Error
}

func insertFileDocumentRevision(
	ctx context.Context,
	tx *gorm.DB,
	seed knowledgeSchemaSeed,
	documentID, revisionID uuid.UUID,
	name string,
) error {
	if err := tx.WithContext(ctx).Exec(
		"INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, status) "+
			"VALUES (?, ?, ?, 'file', ?, 'upload', 'pending')",
		documentID, seed.workspaceID, seed.kbID, name,
	).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Exec(
		"INSERT INTO file_tree_nodes "+
			"(id, workspace_id, knowledge_base_id, parent_id, node_type, name, document_id, document_kind) "+
			"VALUES (?, ?, ?, ?, 'file', ?, ?, 'file')",
		uuid.New(), seed.workspaceID, seed.kbID, seed.rootID, name, documentID,
	).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Exec(
		"INSERT INTO document_revisions "+
			"(id, workspace_id, knowledge_base_id, document_id, kind, revision_no, revision_reason, "+
			"original_filename, file_type, raw_storage_key, processing_version, status) "+
			"VALUES (?, ?, ?, ?, 'file', 1, 'ingest', ?, 'markdown', ?, 1, 'pending')",
		revisionID, seed.workspaceID, seed.kbID, documentID, name, "raw/"+revisionID.String(),
	).Error
}
