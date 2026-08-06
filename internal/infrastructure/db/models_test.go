package db

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type tableNamer interface {
	TableName() string
}

func TestAutoMigratedModelsCoverRequiredTables(t *testing.T) {
	got := map[string]bool{}
	for _, model := range AutoMigratedModels() {
		namer, ok := model.(tableNamer)
		if !ok {
			t.Fatalf("%T does not expose TableName", model)
		}
		got[namer.TableName()] = true
	}

	requiredTables := []string{
		"workspaces",
		"workspace_api_tokens",
		"workspace_api_token_knowledge_bases",
		"model_providers",
		"models",
		"knowledge_bases",
		"knowledge_base_index_generations",
		"documents",
		"document_revisions",
		"faq_revision_contents",
		"faq_revision_questions",
		"file_tree_nodes",
		"document_chunk_sets",
		"chunks",
		"chunk_revisions",
		"document_assets",
		"jobs",
		"retrieval_entries",
		"users",
		"sessions",
		"workspace_memberships",
		"workspace_invitations",
		"workspace_search_settings",
	}
	if len(got) != len(requiredTables) {
		t.Fatalf("AutoMigratedModels returned %d tables, want exactly %d: %v", len(got), len(requiredTables), got)
	}
	for _, required := range requiredTables {
		if !got[required] {
			t.Fatalf("AutoMigratedModels missing table %q", required)
		}
	}
}

func TestKnowledgeBaseRowUsesGenerationAndTreePointers(t *testing.T) {
	t.Parallel()

	rowType := reflect.TypeOf(KnowledgeBaseRow{})
	for _, name := range []string{"ContentVersion", "ActiveIndexGenerationID", "FileTreeRootID"} {
		if _, ok := rowType.FieldByName(name); !ok {
			t.Fatalf("KnowledgeBaseRow missing %s", name)
		}
	}
	if _, ok := rowType.FieldByName("EmbeddingModelID"); ok {
		t.Fatal("KnowledgeBaseRow must not own mutable embedding model configuration")
	}
}

func TestAutoMigratedModelsExcludeFutureIndexTables(t *testing.T) {
	for _, model := range AutoMigratedModels() {
		namer, ok := model.(tableNamer)
		if !ok {
			t.Fatalf("%T does not expose TableName", model)
		}
		switch tableName := namer.TableName(); tableName {
		case "chunk_embeddings", "chunk_keywords":
			t.Fatalf("AutoMigratedModels must not include future index table %q", tableName)
		}

		modelType := reflect.TypeOf(model)
		if modelType.Kind() == reflect.Pointer {
			modelType = modelType.Elem()
		}
		for i := 0; i < modelType.NumField(); i++ {
			tag := modelType.Field(i).Tag.Get("gorm")
			if strings.Contains(strings.ToLower(tag), "type:vector") {
				t.Fatalf("AutoMigratedModels includes vector-backed field %s.%s", modelType.Name(), modelType.Field(i).Name)
			}
		}
	}
}

func TestKnowledgeBaseRowHasWorkspaceID(t *testing.T) {
	field, ok := reflect.TypeOf(KnowledgeBaseRow{}).FieldByName("WorkspaceID")
	if !ok {
		t.Fatal("KnowledgeBaseRow missing WorkspaceID")
	}
	if field.Type != reflect.TypeOf(uuid.UUID{}) {
		t.Fatalf("WorkspaceID type = %s, want uuid.UUID", field.Type)
	}
	tag := field.Tag.Get("gorm")
	if !strings.Contains(tag, "type:uuid") || !strings.Contains(tag, "index") || !strings.Contains(strings.ToLower(tag), "not null") {
		t.Fatalf("WorkspaceID gorm tag = %q, want type:uuid, not null and index", tag)
	}
}

func TestDocumentRevisionRowOwnsRawStorageFields(t *testing.T) {
	documentType := reflect.TypeOf(DocumentRow{})
	for _, name := range []string{"FileType", "RawStorageKey", "SizeBytes", "ContentType", "ParseManifest"} {
		if _, ok := documentType.FieldByName(name); ok {
			t.Fatalf("DocumentRow must not contain revision-local field %s", name)
		}
	}

	rowType := reflect.TypeOf(DocumentRevisionRow{})

	rawStorageKey, ok := rowType.FieldByName("RawStorageKey")
	if !ok {
		t.Fatal("DocumentRow missing RawStorageKey")
	}
	if rawStorageKey.Type != reflect.TypeOf((*string)(nil)) {
		t.Fatalf("RawStorageKey type = %s, want *string", rawStorageKey.Type)
	}

	sizeBytes, ok := rowType.FieldByName("SizeBytes")
	if !ok {
		t.Fatal("DocumentRow missing SizeBytes")
	}
	if sizeBytes.Type.Kind() != reflect.Int64 {
		t.Fatalf("SizeBytes type = %s, want int64", sizeBytes.Type)
	}

	contentType, ok := rowType.FieldByName("ContentType")
	if !ok {
		t.Fatal("DocumentRow missing ContentType")
	}
	if contentType.Type != reflect.TypeOf((*string)(nil)) {
		t.Fatalf("ContentType type = %s, want *string", contentType.Type)
	}

	// v0.7.0: parser_raw_markdown_key 记录 MinerU 产出的原始 Markdown storage key。
	rawMarkdownKey, ok := rowType.FieldByName("ParserRawMarkdownKey")
	if !ok {
		t.Fatal("DocumentRevisionRow missing ParserRawMarkdownKey")
	}
	if rawMarkdownKey.Type != reflect.TypeOf((*string)(nil)) {
		t.Fatalf("ParserRawMarkdownKey type = %s, want *string", rawMarkdownKey.Type)
	}
}

func TestV2RowsSeparateChunkIdentityFromEffectiveContent(t *testing.T) {
	chunkType := reflect.TypeOf(ChunkRow{})
	for _, name := range []string{"Content", "EmbeddingContent", "ContextHeader"} {
		if _, ok := chunkType.FieldByName(name); ok {
			t.Fatalf("ChunkRow must not contain revision-local field %s", name)
		}
	}
	revisionType := reflect.TypeOf(ChunkRevisionRow{})
	for _, name := range []string{"Content", "EmbeddingContent", "ContextHeader", "EditSource", "EditorUserID"} {
		if _, ok := revisionType.FieldByName(name); !ok {
			t.Fatalf("ChunkRevisionRow missing %s", name)
		}
	}
}

func TestWorkspaceRowHasSlug(t *testing.T) {
	slug, ok := reflect.TypeOf(WorkspaceRow{}).FieldByName("Slug")
	if !ok {
		t.Fatal("WorkspaceRow missing Slug")
	}
	if slug.Type.Kind() != reflect.String {
		t.Fatalf("Slug type = %s, want string", slug.Type)
	}
}

func TestUserRowHasAuthFields(t *testing.T) {
	rowType := reflect.TypeOf(UserRow{})

	for _, name := range []string{"ID", "Email", "Nickname", "PasswordHash", "IsPlatformAdmin", "LastLoginAt"} {
		if _, ok := rowType.FieldByName(name); !ok {
			t.Fatalf("UserRow missing %s", name)
		}
	}

	email, _ := rowType.FieldByName("Email")
	if !strings.Contains(email.Tag.Get("gorm"), "unique") {
		t.Fatalf("UserRow.Email gorm tag = %q, want unique", email.Tag.Get("gorm"))
	}

	lastLogin, ok := rowType.FieldByName("LastLoginAt")
	if !ok {
		t.Fatal("UserRow missing LastLoginAt")
	}
	if lastLogin.Type != reflect.TypeOf(&time.Time{}) {
		t.Fatalf("UserRow.LastLoginAt type = %s, want *time.Time", lastLogin.Type)
	}
}

func TestSessionRowHasAuthFields(t *testing.T) {
	rowType := reflect.TypeOf(SessionRow{})

	for _, name := range []string{"ID", "UserID", "ExpiresAt", "CreatedAt", "LastSeenAt", "UserAgent", "IPAddr", "RevokedAt"} {
		if _, ok := rowType.FieldByName(name); !ok {
			t.Fatalf("SessionRow missing %s", name)
		}
	}

	ip, _ := rowType.FieldByName("IPAddr")
	if ip.Type.Kind() != reflect.String {
		t.Fatalf("SessionRow.IPAddr type = %s, want string", ip.Type)
	}
	if !strings.Contains(ip.Tag.Get("gorm"), "type:inet") {
		t.Fatalf("SessionRow.IPAddr gorm tag = %q, want type:inet", ip.Tag.Get("gorm"))
	}

	revoked, _ := rowType.FieldByName("RevokedAt")
	if revoked.Type != reflect.TypeOf(&time.Time{}) {
		t.Fatalf("SessionRow.RevokedAt type = %s, want *time.Time", revoked.Type)
	}
}

func TestMembershipRowHasAuthFields(t *testing.T) {
	rowType := reflect.TypeOf(MembershipRow{})

	for _, name := range []string{"ID", "WorkspaceID", "UserID", "Role", "CreatedAt", "UpdatedAt"} {
		if _, ok := rowType.FieldByName(name); !ok {
			t.Fatalf("MembershipRow missing %s", name)
		}
	}
	workspaceID, _ := rowType.FieldByName("WorkspaceID")
	if !strings.Contains(workspaceID.Tag.Get("gorm"), "type:uuid") {
		t.Fatalf("MembershipRow.WorkspaceID gorm tag = %q, want type:uuid", workspaceID.Tag.Get("gorm"))
	}
}

func TestInvitationRowHasAuthFields(t *testing.T) {
	rowType := reflect.TypeOf(InvitationRow{})

	for _, name := range []string{
		"ID", "WorkspaceID", "InvitedEmail", "Role", "TokenHash", "TokenPrefix",
		"ExpiresAt", "AcceptedAt", "AcceptedUserID", "RevokedAt", "CreatedBy", "CreatedAt",
	} {
		if _, ok := rowType.FieldByName(name); !ok {
			t.Fatalf("InvitationRow missing %s", name)
		}
	}

	acceptedUserID, ok := rowType.FieldByName("AcceptedUserID")
	if !ok {
		t.Fatal("InvitationRow missing AcceptedUserID")
	}
	// 可空 uuid 用指针表示
	if acceptedUserID.Type != reflect.TypeOf(&uuid.UUID{}) {
		t.Fatalf("InvitationRow.AcceptedUserID type = %s, want *uuid.UUID", acceptedUserID.Type)
	}
}
