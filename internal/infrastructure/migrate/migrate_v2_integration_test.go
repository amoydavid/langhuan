//go:build integration

package migrate

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/dajee/langhuan/internal/testsupport"
)

func TestKnowledgeRetrievalV2UpDownUpPreservesTenantAndModelData(t *testing.T) {
	ctx := context.Background()
	// 复用 testcontainers 临时容器作为可建库的 baseDSN，严禁回退到 config.yaml
	// 的开发库或本机长期运行的 PostgreSQL（AGENTS 5.10）。
	baseDSN := testsupport.NewEmptyPostgres(t)
	testDatabaseName := "langhuan_migration_v2_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	adminDSN := withDatabaseName(t, baseDSN, "postgres")
	baseDB, err := sql.Open("postgres", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer baseDB.Close()

	if _, err := baseDB.ExecContext(ctx, "CREATE DATABASE "+testDatabaseName); err != nil {
		t.Fatalf("create isolated database: %v", err)
	}
	t.Cleanup(func() {
		cleanupDB, openErr := sql.Open("postgres", adminDSN)
		if openErr != nil {
			t.Errorf("open cleanup database: %v", openErr)
			return
		}
		defer cleanupDB.Close()
		if _, dropErr := cleanupDB.ExecContext(
			context.Background(),
			"DROP DATABASE IF EXISTS "+testDatabaseName+" WITH (FORCE)",
		); dropErr != nil {
			t.Errorf("drop isolated database: %v", dropErr)
		}
	})

	scopedDSN := withDatabaseName(t, baseDSN, testDatabaseName)
	scopedDB, err := sql.Open("postgres", scopedDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer scopedDB.Close()

	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := postgres.WithInstance(scopedDB, &postgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()

	if err := migrator.Migrate(4); err != nil {
		t.Fatalf("migrate to version 4: %v", err)
	}
	seed := seedVersion4Data(t, ctx, scopedDB)
	if err := migrator.Migrate(5); err != nil {
		t.Fatalf("migrate to version 5: %v", err)
	}
	assertPreservedRows(t, ctx, scopedDB, seed)

	if err := migrator.Migrate(4); err != nil {
		t.Fatalf("migrate down to version 4: %v", err)
	}
	if err := migrator.Migrate(5); err != nil {
		t.Fatalf("migrate up to version 5 again: %v", err)
	}
	assertPreservedRows(t, ctx, scopedDB, seed)

	var oldKnowledgeBaseCount int
	if err := scopedDB.QueryRowContext(
		ctx,
		"SELECT count(*) FROM knowledge_bases WHERE id = $1",
		seed.knowledgeBaseID,
	).Scan(&oldKnowledgeBaseCount); err != nil {
		t.Fatal(err)
	}
	if oldKnowledgeBaseCount != 0 {
		t.Fatalf("destructive migration retained %d old knowledge bases", oldKnowledgeBaseCount)
	}
}

func TestGenerationFailedCountMigrationUpDownUp(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("postgres", testsupport.NewEmptyPostgres(t))
	if err != nil {
		t.Fatal(err)
	}

	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := postgres.WithInstance(database, &postgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()

	if err := migrator.Migrate(8); err != nil {
		t.Fatalf("migrate to version 8: %v", err)
	}
	assertGenerationFailedCountColumn(t, ctx, database, false)

	if err := migrator.Migrate(7); err != nil {
		t.Fatalf("migrate down to version 7: %v", err)
	}
	assertGenerationFailedCountColumn(t, ctx, database, true)

	if err := migrator.Migrate(8); err != nil {
		t.Fatalf("migrate up to version 8 again: %v", err)
	}
	assertGenerationFailedCountColumn(t, ctx, database, false)
}

func TestActiveGenerationStatsMigrationBackfillsPublishedProjection(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("postgres", testsupport.NewEmptyPostgres(t))
	if err != nil {
		t.Fatal(err)
	}

	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := postgres.WithInstance(database, &postgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()

	if err := migrator.Migrate(8); err != nil {
		t.Fatalf("migrate to version 8: %v", err)
	}
	generationID := seedStaleActiveGenerationStats(t, ctx, database)

	if err := migrator.Migrate(9); err != nil {
		t.Fatalf("migrate to version 9: %v", err)
	}

	var documentCount, chunkCount, indexedCount, manualEditCount, disabledChunkCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT document_count, chunk_count, indexed_count, manual_edit_count, disabled_chunk_count
		FROM knowledge_base_index_generations
		WHERE id = $1
	`, generationID).Scan(
		&documentCount, &chunkCount, &indexedCount, &manualEditCount, &disabledChunkCount,
	); err != nil {
		t.Fatal(err)
	}
	if documentCount != 1 || chunkCount != 2 || indexedCount != 1 ||
		manualEditCount != 1 || disabledChunkCount != 1 {
		t.Fatalf(
			"backfilled stats = documents %d chunks %d indexed %d manual %d disabled %d",
			documentCount, chunkCount, indexedCount, manualEditCount, disabledChunkCount,
		)
	}
}

func seedStaleActiveGenerationStats(t *testing.T, ctx context.Context, database *sql.DB) uuid.UUID {
	t.Helper()
	workspaceID, userID := uuid.New(), uuid.New()
	providerID, modelID := uuid.New(), uuid.New()
	knowledgeBaseID, rootID, generationID := uuid.New(), uuid.New(), uuid.New()
	documentID, documentRevisionID, chunkSetID := uuid.New(), uuid.New(), uuid.New()
	indexedChunkID, indexedRevisionID := uuid.New(), uuid.New()
	editedChunkID, baseRevisionID, editedRevisionID := uuid.New(), uuid.New(), uuid.New()

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO workspaces (id, name, slug) VALUES ($1, 'stats migration', $2)", []any{workspaceID, "stats-" + uuid.NewString()}},
		{"INSERT INTO users (id, email, nickname, password_hash) VALUES ($1, $2, 'stats migration', 'hash')", []any{userID, uuid.NewString() + "@example.com"}},
		{"INSERT INTO model_providers (id, scope, workspace_id, name, provider, status, created_by) " +
			"VALUES ($1, 'workspace', $2, $3, 'openai', 'active', $4)", []any{providerID, workspaceID, "provider-" + uuid.NewString(), userID}},
		{"INSERT INTO models (id, provider_id, name, type, model_name, dimensions, status, created_by) " +
			"VALUES ($1, $2, $3, 'embedding', 'text-embedding', 1024, 'active', $4)", []any{modelID, providerID, "model-" + uuid.NewString(), userID}},
		{"INSERT INTO knowledge_bases (id, workspace_id, name, content_version, active_index_generation_id, file_tree_root_id) " +
			"VALUES ($1, $2, 'stats migration', 1, $3, $4)", []any{knowledgeBaseID, workspaceID, generationID, rootID}},
		{"INSERT INTO knowledge_base_index_generations " +
			"(id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, embedding_dimension, " +
			"model_config_hash, chunker_version, chunking_config, config_hash, indexed_content_version, status) " +
			"VALUES ($1, $2, $3, $4, $5, 'text-embedding', 1024, 'model-hash', 2, " +
			"'{\"chunk_size\":512,\"chunk_overlap\":80}'::jsonb, 'config-hash', 1, 'ready')",
			[]any{generationID, workspaceID, knowledgeBaseID, modelID, providerID}},
		{"INSERT INTO documents " +
			"(id, workspace_id, knowledge_base_id, kind, title, source_type, status, active_revision_id) " +
			"VALUES ($1, $2, $3, 'file', 'stats.docx', 'upload', 'ready', $4)",
			[]any{documentID, workspaceID, knowledgeBaseID, documentRevisionID}},
		{"INSERT INTO document_revisions " +
			"(id, workspace_id, knowledge_base_id, document_id, kind, revision_no, revision_reason, original_filename, " +
			"file_type, raw_storage_key, processing_version, status, completed_at) " +
			"VALUES ($1, $2, $3, $4, 'file', 1, 'ingest', 'stats.docx', 'docx', 'raw/stats.docx', 1, 'ready', now())",
			[]any{documentRevisionID, workspaceID, knowledgeBaseID, documentID}},
		{"INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) " +
			"VALUES ($1, $2, $3, 'root', '')", []any{rootID, workspaceID, knowledgeBaseID}},
		{"INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name, document_id, document_kind) " +
			"VALUES ($1, $2, $3, $4, 'file', 'stats.docx', $5, 'file')", []any{uuid.New(), workspaceID, knowledgeBaseID, rootID, documentID}},
		{"INSERT INTO document_chunk_sets " +
			"(id, workspace_id, knowledge_base_id, document_id, document_revision_id, strategy, chunker_version, " +
			"chunking_config, config_hash, status, chunk_count, ready_at) " +
			"VALUES ($1, $2, $3, $4, $5, 'standard', 2, '{\"chunk_size\":512,\"chunk_overlap\":80}'::jsonb, " +
			"'chunk-config-hash', 'ready', 2, now())",
			[]any{chunkSetID, workspaceID, knowledgeBaseID, documentID, documentRevisionID}},
		{"INSERT INTO chunks " +
			"(id, workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, sequence, " +
			"source_content, active_revision_id) VALUES ($1, $2, $3, $4, $5, $6, 0, 'indexed source', $7)",
			[]any{indexedChunkID, workspaceID, knowledgeBaseID, documentID, documentRevisionID, chunkSetID, indexedRevisionID}},
		{"INSERT INTO chunk_revisions " +
			"(id, workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id, " +
			"revision_no, content, embedding_content, enabled, status, edit_source, indexed_at) " +
			"VALUES ($1, $2, $3, $4, $5, $6, $7, 1, 'indexed content', 'indexed content', true, 'ready', 'system', now())",
			[]any{indexedRevisionID, workspaceID, knowledgeBaseID, documentID, documentRevisionID, chunkSetID, indexedChunkID}},
		{"INSERT INTO chunks " +
			"(id, workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, sequence, " +
			"source_content, active_revision_id) VALUES ($1, $2, $3, $4, $5, $6, 1, 'edited source', $7)",
			[]any{editedChunkID, workspaceID, knowledgeBaseID, documentID, documentRevisionID, chunkSetID, editedRevisionID}},
		{"INSERT INTO chunk_revisions " +
			"(id, workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id, " +
			"revision_no, content, embedding_content, enabled, status, edit_source, indexed_at) " +
			"VALUES ($1, $2, $3, $4, $5, $6, $7, 1, 'base content', 'base content', true, 'ready', 'system', now())",
			[]any{baseRevisionID, workspaceID, knowledgeBaseID, documentID, documentRevisionID, chunkSetID, editedChunkID}},
		{"INSERT INTO chunk_revisions " +
			"(id, workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id, revision_no, " +
			"base_revision_id, content, embedding_content, enabled, status, edit_source, editor_user_id, indexed_at) " +
			"VALUES ($1, $2, $3, $4, $5, $6, $7, 2, $8, '', '', false, 'ready', 'user', $9, now())",
			[]any{editedRevisionID, workspaceID, knowledgeBaseID, documentID, documentRevisionID, chunkSetID, editedChunkID, baseRevisionID, userID}},
		{"INSERT INTO retrieval_entries " +
			"(id, workspace_id, knowledge_base_id, index_generation_id, document_id, document_revision_id, chunk_set_id, " +
			"chunk_id, chunk_revision_id, state, search_content, content, fts_document, embedding, dimension, published_at) " +
			"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'published', 'indexed content', 'indexed content', " +
			"to_tsvector('simple', 'indexed content'), " +
			"('[' || '1,' || repeat('0,', 1022) || '0]')::halfvec, 1024, now())",
			[]any{uuid.New(), workspaceID, knowledgeBaseID, generationID, documentID, documentRevisionID, chunkSetID, indexedChunkID, indexedRevisionID}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed stale active Generation stats: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit stale active Generation stats: %v", err)
	}
	return generationID
}

func assertGenerationFailedCountColumn(t *testing.T, ctx context.Context, database *sql.DB, want bool) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'knowledge_base_index_generations'
			  AND column_name = 'failed_count'
		)
	`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != want {
		t.Fatalf("failed_count column exists = %v, want %v", exists, want)
	}
}

type version4Seed struct {
	workspaceID     uuid.UUID
	userID          uuid.UUID
	providerID      uuid.UUID
	modelID         uuid.UUID
	knowledgeBaseID uuid.UUID
}

func seedVersion4Data(t *testing.T, ctx context.Context, database *sql.DB) version4Seed {
	t.Helper()
	seed := version4Seed{
		workspaceID: uuid.New(), userID: uuid.New(), providerID: uuid.New(),
		modelID: uuid.New(), knowledgeBaseID: uuid.New(),
	}
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO workspaces (id, name, slug) VALUES ($1, 'preserved', $2)", []any{seed.workspaceID, "preserved-" + uuid.NewString()}},
		{"INSERT INTO users (id, email, nickname, password_hash) VALUES ($1, $2, 'preserved', 'hash')", []any{seed.userID, uuid.NewString() + "@example.com"}},
		{"INSERT INTO workspace_memberships (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", []any{seed.workspaceID, seed.userID}},
		{"INSERT INTO model_providers (id, scope, workspace_id, name, provider, created_by) " +
			"VALUES ($1, 'workspace', $2, $3, 'openai', $4)", []any{seed.providerID, seed.workspaceID, "provider-" + uuid.NewString(), seed.userID}},
		{"INSERT INTO models (id, provider_id, name, type, model_name, dimensions, created_by) " +
			"VALUES ($1, $2, $3, 'embedding', 'text-embedding', 1024, $4)", []any{seed.modelID, seed.providerID, "model-" + uuid.NewString(), seed.userID}},
		{"INSERT INTO knowledge_bases (id, workspace_id, name, embedding_model_id) " +
			"VALUES ($1, $2, 'old knowledge', $3)", []any{seed.knowledgeBaseID, seed.workspaceID, seed.modelID}},
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed version 4: %v", err)
		}
	}
	return seed
}

func assertPreservedRows(t *testing.T, ctx context.Context, database *sql.DB, seed version4Seed) {
	t.Helper()
	checks := []struct {
		table string
		id    uuid.UUID
	}{
		{"workspaces", seed.workspaceID},
		{"users", seed.userID},
		{"model_providers", seed.providerID},
		{"models", seed.modelID},
	}
	for _, check := range checks {
		var count int
		if err := database.QueryRowContext(
			ctx,
			"SELECT count(*) FROM "+check.table+" WHERE id = $1",
			check.id,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s preserved rows = %d, want 1", check.table, count)
		}
	}

	var membershipCount int
	if err := database.QueryRowContext(
		ctx,
		"SELECT count(*) FROM workspace_memberships WHERE workspace_id = $1 AND user_id = $2",
		seed.workspaceID,
		seed.userID,
	).Scan(&membershipCount); err != nil {
		t.Fatal(err)
	}
	if membershipCount != 1 {
		t.Fatalf("workspace membership count = %d, want 1", membershipCount)
	}
}

func withDatabaseName(t *testing.T, dsn, databaseName string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + databaseName
	return parsed.String()
}
