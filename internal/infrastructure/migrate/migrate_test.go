package migrate

import (
	"io"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// newSource 构建一个基于 embed FS 的 iofs source driver，用于验证迁移文件
// 被正确解析。不连接任何数据库。
func newSource(t *testing.T) source.Driver {
	t.Helper()
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("iofs.New() error = %v", err)
	}
	return src
}

func TestMigrationSourceContainsInitVersion(t *testing.T) {
	src := newSource(t)

	first, err := src.First()
	if err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if first != 1 {
		t.Fatalf("first version = %d, want 1", first)
	}
}

func TestMigrationSourceHasUpAndDown(t *testing.T) {
	src := newSource(t)

	up, idUp, err := src.ReadUp(1)
	if err != nil {
		t.Fatalf("ReadUp(1) error = %v", err)
	}
	upBody := readAll(t, up)
	if upBody == "" {
		t.Fatal("ReadUp(1) 返回空内容")
	}
	if idUp == "" {
		t.Fatal("ReadUp(1) 返回空 identifier")
	}

	down, idDown, err := src.ReadDown(1)
	if err != nil {
		t.Fatalf("ReadDown(1) error = %v", err)
	}
	downBody := readAll(t, down)
	if downBody == "" {
		t.Fatal("ReadDown(1) 返回空内容")
	}
	if idDown == "" {
		t.Fatal("ReadDown(1) 返回空 identifier")
	}
}

func TestMigrationSourceUpContainsExtensionsAndTables(t *testing.T) {
	src := newSource(t)
	up, _, err := src.ReadUp(1)
	if err != nil {
		t.Fatalf("ReadUp(1) error = %v", err)
	}
	body := readAll(t, up)

	for _, required := range []string{
		"CREATE EXTENSION IF NOT EXISTS vector",
		"CREATE EXTENSION IF NOT EXISTS pgcrypto",
		"CREATE TABLE IF NOT EXISTS workspaces",
		"CREATE TABLE IF NOT EXISTS workspace_api_tokens",
		"CREATE TABLE IF NOT EXISTS knowledge_bases",
		"workspace_id uuid",
		"raw_storage_key text",
		"size_bytes bigint",
		"content_type text",
		"CREATE TABLE IF NOT EXISTS document_assets",
		"CREATE TABLE IF NOT EXISTS jobs",
		"CREATE TABLE IF NOT EXISTS chunk_embeddings",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("up migration 缺少 %q", required)
		}
	}
}

// TestMigrationSourceHasSecondVersion 断言 v1 之后存在 version 2。
// src.Up(current) 返回下一个可用的版本号。
func TestMigrationSourceHasSecondVersion(t *testing.T) {
	src := newSource(t)

	first, err := src.First()
	if err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if first != 1 {
		t.Fatalf("first version = %d, want 1", first)
	}

	next, err := src.Next(first)
	if err != nil {
		t.Fatalf("Next(1) error = %v", err)
	}
	if next != 2 {
		t.Fatalf("Next(1) = %d, want 2", next)
	}
}

func TestMigrationSourceVersion3AddsParsingSchema(t *testing.T) {
	src := newSource(t)
	up, _, err := src.ReadUp(3)
	if err != nil {
		t.Fatalf("ReadUp(3) error = %v", err)
	}
	body := readAll(t, up)
	for _, required := range []string{"processing_version", "parse_manifest", "embedding_content"} {
		if !strings.Contains(body, required) {
			t.Fatalf("version 3 migration missing %q", required)
		}
	}
}

func TestMigrationSourceVersion3DownRemovesParsingSchema(t *testing.T) {
	src := newSource(t)
	down, _, err := src.ReadDown(3)
	if err != nil {
		t.Fatalf("ReadDown(3) error = %v", err)
	}
	body := readAll(t, down)
	for _, required := range []string{
		"DROP COLUMN IF EXISTS embedding_content",
		"DROP COLUMN IF EXISTS parse_manifest",
		"DROP COLUMN IF EXISTS processing_version",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("version 3 down migration missing %q", required)
		}
	}
}

func TestMigrationSourceVersion4DefinesModelConfiguration(t *testing.T) {
	src := newSource(t)
	up, _, err := src.ReadUp(4)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, up)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS model_providers",
		"credentials_ciphertext bytea",
		"CREATE TABLE IF NOT EXISTS models",
		"dimensions IN (798, 1024, 2048, 3584)",
		"DROP COLUMN IF EXISTS embedding_dimension",
		"ADD COLUMN embedding_model_id uuid NOT NULL",
		"REFERENCES models(id) ON DELETE RESTRICT",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("version 4 migration missing %q", required)
		}
	}
	if strings.Contains(body, "provider IN (") {
		t.Fatal("provider must not have an enumerating CHECK constraint")
	}
}

func TestMigrationSourceVersion4DownRestoresKnowledgeBaseDimension(t *testing.T) {
	src := newSource(t)
	down, _, err := src.ReadDown(4)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, down)
	for _, required := range []string{
		"ADD COLUMN embedding_dimension integer",
		"SET embedding_dimension = m.dimensions",
		"ALTER COLUMN embedding_dimension SET DEFAULT 1024",
		"DROP COLUMN embedding_model_id",
		"DROP TABLE IF EXISTS models",
		"DROP TABLE IF EXISTS model_providers",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("version 4 down migration missing %q", required)
		}
	}
}

func TestMigrationSourceVersion5DefinesKnowledgeRetrievalV2(t *testing.T) {
	src := newSource(t)
	up, _, err := src.ReadUp(5)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, up)
	for _, fragment := range []string{
		"CREATE TABLE knowledge_bases",
		"CREATE TABLE file_tree_nodes",
		"CREATE TABLE document_revisions",
		"CREATE TABLE faq_revision_contents",
		"CREATE TABLE faq_revision_questions",
		"CREATE TABLE document_chunk_sets",
		"CREATE TABLE chunk_revisions",
		"CREATE TABLE knowledge_base_index_generations",
		"CREATE TABLE retrieval_entries",
		"workspace_id uuid NOT NULL",
		"kind text NOT NULL",
		"search_content text NOT NULL",
		"fts_document tsvector",
		"embedding halfvec",
		"idx_retrieval_entries_hnsw_1024",
		"idx_retrieval_entries_fts",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("version 5 migration missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"CREATE TABLE chunk_embeddings",
		"CREATE TABLE chunk_keywords",
		"ENABLE ROW LEVEL SECURITY",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("version 5 migration contains forbidden %q", forbidden)
		}
	}
}

func TestMigrationSourceVersion6MakesGenerationBaseRetentionSafe(t *testing.T) {
	src := newSource(t)
	up, _, err := src.ReadUp(6)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, up)
	for _, fragment := range []string{
		"DROP CONSTRAINT IF EXISTS index_generations_base_fk",
		"REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)",
		"ON DELETE SET NULL (base_generation_id)",
		"DEFERRABLE INITIALLY DEFERRED",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("version 6 migration missing %q", fragment)
		}
	}
}

func TestMigrationSourceVersion7RepairsAppliedGenerationRetentionFK(t *testing.T) {
	src := newSource(t)
	up, _, err := src.ReadUp(7)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, up)
	if !strings.Contains(body, "ON DELETE SET NULL (base_generation_id)") {
		t.Fatal("version 7 must narrow SET NULL to base_generation_id")
	}
}

func TestMigrationSourceVersion8DropsGenerationFailedCount(t *testing.T) {
	src := newSource(t)
	up, _, err := src.ReadUp(8)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, up)
	for _, fragment := range []string{
		"DROP CONSTRAINT IF EXISTS index_generations_count_check",
		"DROP COLUMN IF EXISTS failed_count",
		"ADD CONSTRAINT index_generations_count_check",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("version 8 migration missing %q", fragment)
		}
	}
	if strings.Contains(body, "failed_count >= 0") {
		t.Fatal("version 8 count constraint must not reference failed_count")
	}
}

func TestMigrationSourceVersion8DownRestoresGenerationFailedCount(t *testing.T) {
	src := newSource(t)
	down, _, err := src.ReadDown(8)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, down)
	for _, fragment := range []string{
		"ADD COLUMN failed_count bigint NOT NULL DEFAULT 0",
		"failed_count >= 0",
		"failed_count <= chunk_count",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("version 8 down migration missing %q", fragment)
		}
	}
}

func TestMigrationSourceVersion9BackfillsActiveGenerationStats(t *testing.T) {
	src := newSource(t)
	up, _, err := src.ReadUp(9)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, up)
	for _, fragment := range []string{
		"active_generations",
		"document_count",
		"chunk_count",
		"indexed_count",
		"manual_edit_count",
		"disabled_chunk_count",
		"RetrievalEntry stats are derived",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("version 9 migration missing %q", fragment)
		}
	}
}

// TestMigrationSourceVersion10RebuildsWorkspaceAPIKeys 断言 v0.6.0 重建后的
// API Key 表、绑定表、scope/到期/前缀约束与复合外键。
func TestMigrationSourceVersion10RebuildsWorkspaceAPIKeys(t *testing.T) {
	src := newSource(t)
	up, _, err := src.ReadUp(10)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, up)
	for _, fragment := range []string{
		"DROP TABLE IF EXISTS workspace_api_token_knowledge_bases",
		"DROP TABLE IF EXISTS workspace_api_tokens",
		"CREATE TABLE workspace_api_tokens",
		"token_secret_ciphertext",
		"bytea NOT NULL",
		"text[] NOT NULL",
		"expires_at timestamptz",
		"created_by uuid REFERENCES users(id)",
		"revoked_by uuid REFERENCES users(id)",
		"UNIQUE (id, workspace_id)",
		"workspace_api_tokens_hash_check",
		"workspace_api_tokens_prefix_check",
		"workspace_api_tokens_scopes_check",
		"workspace_api_tokens_expiry_check",
		"CREATE UNIQUE INDEX idx_workspace_api_tokens_token_hash",
		"CREATE TABLE workspace_api_token_knowledge_bases",
		"FOREIGN KEY (api_token_id, workspace_id)",
		"REFERENCES workspace_api_tokens(id, workspace_id) ON DELETE CASCADE",
		"FOREIGN KEY (workspace_id, knowledge_base_id)",
		"REFERENCES knowledge_bases(workspace_id, id)",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("version 10 migration missing %q", fragment)
		}
	}
}

// TestMigrationSourceVersion10DownRestoresPlaceholder 断言 down 恢复 000001
// 占位表结构，使 down/up 可重复执行。
func TestMigrationSourceVersion10DownRestoresPlaceholder(t *testing.T) {
	src := newSource(t)
	down, _, err := src.ReadDown(10)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, down)
	for _, fragment := range []string{
		"DROP TABLE IF EXISTS workspace_api_token_knowledge_bases",
		"DROP TABLE IF EXISTS workspace_api_tokens",
		"CREATE TABLE workspace_api_tokens",
		"CREATE INDEX idx_workspace_api_tokens_workspace_id",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("version 10 down migration missing %q", fragment)
		}
	}
}

// TestMigrationSourceVersion2UpContainsAuthSchema 读取 version 2 的 up 脚本，
// 断言它创建多租户认证所需的四张表、角色 CHECK 约束、token-hash/session 索引
// 以及 workspace slug 唯一索引。
func TestMigrationSourceVersion2UpContainsAuthSchema(t *testing.T) {
	src := newSource(t)

	up, idUp, err := src.ReadUp(2)
	if err != nil {
		t.Fatalf("ReadUp(2) error = %v", err)
	}
	upBody := readAll(t, up)
	if upBody == "" {
		t.Fatal("ReadUp(2) 返回空内容")
	}
	if idUp == "" {
		t.Fatal("ReadUp(2) 返回空 identifier")
	}

	for _, required := range []string{
		"CREATE TABLE users",
		"CREATE TABLE sessions",
		"CREATE TABLE workspace_memberships",
		"CREATE TABLE workspace_invitations",
		"workspace_memberships_role_check",
		"workspace_invitations_role_check",
		"idx_invitations_token_hash",
		"idx_sessions_user_id",
		"idx_workspaces_slug",
		// slug 回填 + NOT NULL 流程
		"ADD COLUMN IF NOT EXISTS slug text",
		"ALTER COLUMN slug SET NOT NULL",
	} {
		if !strings.Contains(upBody, required) {
			t.Fatalf("version 2 up migration 缺少 %q", required)
		}
	}

	// slug 唯一索引必须带 unique
	if !strings.Contains(upBody, "CREATE UNIQUE INDEX") {
		t.Fatal("version 2 up migration 缺少 CREATE UNIQUE INDEX（slug）")
	}
}

// TestMigrationSourceVersion2DownReversesAuthObjects 断言 version 2 的 down 脚本
// 只回滚 000002 新增对象，且非空。
func TestMigrationSourceVersion2DownReversesAuthObjects(t *testing.T) {
	src := newSource(t)

	down, idDown, err := src.ReadDown(2)
	if err != nil {
		t.Fatalf("ReadDown(2) error = %v", err)
	}
	downBody := readAll(t, down)
	if downBody == "" {
		t.Fatal("ReadDown(2) 返回空内容")
	}
	if idDown == "" {
		t.Fatal("ReadDown(2) 返回空 identifier")
	}

	for _, required := range []string{
		"DROP TABLE IF EXISTS workspace_invitations",
		"DROP TABLE IF EXISTS workspace_memberships",
		"DROP TABLE IF EXISTS sessions",
		"DROP TABLE IF EXISTS users",
		"idx_workspaces_slug",
	} {
		if !strings.Contains(downBody, required) {
			t.Fatalf("version 2 down migration 缺少 %q", required)
		}
	}

	// down 必须保留 000001 创建的表
	for _, mustNotDrop := range []string{
		"DROP TABLE IF EXISTS workspaces",
		"DROP TABLE IF EXISTS knowledge_bases",
		"DROP TABLE IF EXISTS documents",
	} {
		if strings.Contains(downBody, mustNotDrop) {
			t.Fatalf("version 2 down migration 不得删除 000001 的表: %q", mustNotDrop)
		}
	}
}

// TestMigrationSourceVersion11DefinesZhparser 断言 v0.6.x 引入的 zhparser
// 中文分词：注册扩展、幂等创建 text search configuration 与词性映射。
func TestMigrationSourceVersion11DefinesZhparser(t *testing.T) {
	src := newSource(t)
	up, _, err := src.ReadUp(11)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, up)
	for _, fragment := range []string{
		"CREATE EXTENSION IF NOT EXISTS zhparser WITH SCHEMA public",
		"CREATE TEXT SEARCH CONFIGURATION public.zhparser (PARSER = public.zhparser)",
		"c.cfgnamespace = 'public'::regnamespace",
		"ALTER TEXT SEARCH CONFIGURATION public.zhparser ADD MAPPING",
		"pg_ts_config_map",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("version 11 migration missing %q", fragment)
		}
	}
}

// TestMigrationSourceVersion11DownKeepsZhparser 断言 down 保留配置与扩展，
// 避免已持久化的 Generation retrieval_config 失效。
func TestMigrationSourceVersion11DownKeepsZhparser(t *testing.T) {
	src := newSource(t)
	down, _, err := src.ReadDown(11)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, down)
	for _, forbidden := range []string{
		"DROP TEXT SEARCH CONFIGURATION IF EXISTS zhparser",
		"DROP EXTENSION IF EXISTS zhparser",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("version 11 down migration 不得包含 %q", forbidden)
		}
	}
	if !strings.Contains(body, "SELECT 1") {
		t.Fatal("version 11 down migration 应保留可执行的 no-op")
	}
}

func readAll(t *testing.T, r io.ReadCloser) string {
	t.Helper()
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("读取迁移内容失败: %v", err)
	}
	return string(data)
}
