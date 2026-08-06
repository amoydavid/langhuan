package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestPatchKnowledgeBaseRejectsMemberAndUnknownFields(t *testing.T) {
	member := newSlugResourceFixtures(t, value.RoleMember, false)
	memberID := seedKnowledgeBaseForPatch(t, member)
	memberPath := "/api/v1/workspaces/acme/knowledge-bases/" + memberID.String()
	if recorder := member.authedRequest(stdhttp.MethodPatch, memberPath, []byte(`{"name":"新名称"}`), "application/json"); recorder.Code != stdhttp.StatusForbidden {
		t.Fatalf("member status/body = %d %s", recorder.Code, recorder.Body.String())
	}

	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	adminID := seedKnowledgeBaseForPatch(t, admin)
	adminPath := "/api/v1/workspaces/acme/knowledge-bases/" + adminID.String()
	if recorder := admin.authedRequest(stdhttp.MethodPatch, adminPath, []byte(`{"name":"新名称","metadata":{}}`), "application/json"); recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("unknown field status/body = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestPatchKnowledgeBaseUpdatesOnlyProvidedBasics(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	id := seedKnowledgeBaseForPatch(t, admin)
	path := "/api/v1/workspaces/acme/knowledge-bases/" + id.String()
	recorder := admin.authedRequest(stdhttp.MethodPatch, path, []byte(`{"description":"新的描述"}`), "application/json")
	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	updated := admin.kbSvc.items[id]
	if updated.Name != "原名称" || updated.Description != "新的描述" {
		t.Fatalf("updated = %#v", updated)
	}
	if admin.kbSvc.updateInput.ActorRole != value.RoleAdmin || admin.kbSvc.updateInput.WorkspaceID != admin.wsID {
		t.Fatalf("update input = %#v", admin.kbSvc.updateInput)
	}
}

func TestPatchKnowledgeBaseRejectsEmptyObjectAndBlankName(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	id := seedKnowledgeBaseForPatch(t, admin)
	path := "/api/v1/workspaces/acme/knowledge-bases/" + id.String()
	for _, payload := range []string{`{}`, `{"name":"   "}`} {
		recorder := admin.authedRequest(stdhttp.MethodPatch, path, []byte(payload), "application/json")
		if recorder.Code != stdhttp.StatusBadRequest {
			t.Fatalf("payload %s status/body = %d %s", payload, recorder.Code, recorder.Body.String())
		}
	}
}

func seedKnowledgeBaseForPatch(t *testing.T, fixtures *slugResourceFixtures) uuid.UUID {
	t.Helper()
	created, err := fixtures.kbSvc.Create(context.Background(), service.CreateKnowledgeBaseInput{
		WorkspaceID: fixtures.wsID, Name: "原名称", Description: "原描述", EmbeddingModelID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return created.ID
}

// TestCreateKnowledgeBasePassesSourceFields 验证 create 端点透传 source_type/source_config/source_connection_id，
// 并在响应中回显来源信息。
func TestCreateKnowledgeBasePassesSourceFields(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	modelID := uuid.New()
	connID := uuid.New()
	body := []byte(`{"name":"飞书库","embedding_model_id":"` + modelID.String() + `","source_type":"feishu_wiki","source_config":{"root_token":"root-tok","root_kind":"wiki_node"},"source_connection_id":"` + connID.String() + `"}`)
	recorder := admin.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases", body, "application/json")
	if recorder.Code != stdhttp.StatusCreated {
		t.Fatalf("status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	if admin.kbSvc.createInput == nil {
		t.Fatal("create not invoked")
	}
	if admin.kbSvc.createInput.SourceType != value.SourceTypeFeishuWiki {
		t.Fatalf("source type = %q, want feishu_wiki", admin.kbSvc.createInput.SourceType)
	}
	if admin.kbSvc.createInput.SourceConnectionID == nil || *admin.kbSvc.createInput.SourceConnectionID != connID {
		t.Fatalf("source connection id = %v, want %s", admin.kbSvc.createInput.SourceConnectionID, connID)
	}
	if admin.kbSvc.createInput.SourceConfig["root_token"] != "root-tok" {
		t.Fatalf("source config = %#v", admin.kbSvc.createInput.SourceConfig)
	}
	var created dto.KnowledgeBase
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.SourceType != value.SourceTypeFeishuWiki {
		t.Fatalf("response source type = %q, want feishu_wiki", created.SourceType)
	}
}

// TestCreateKnowledgeBaseRejectsFeishuWithoutConnectionID 验证飞书来源缺失 connection_id 返回 400。
func TestCreateKnowledgeBaseRejectsFeishuWithoutConnectionID(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	modelID := uuid.New()
	body := []byte(`{"name":"飞书库","embedding_model_id":"` + modelID.String() + `","source_type":"feishu_wiki","source_config":{"root_token":"root-tok"}}`)
	recorder := admin.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases", body, "application/json")
	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status/body = %d %s, want 400", recorder.Code, recorder.Body.String())
	}
}

// TestCreateKnowledgeBaseRejectsInvalidSourceType 验证未知 source_type 返回 400。
func TestCreateKnowledgeBaseRejectsInvalidSourceType(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	modelID := uuid.New()
	body := []byte(`{"name":"库","embedding_model_id":"` + modelID.String() + `","source_type":"unknown"}`)
	recorder := admin.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases", body, "application/json")
	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status/body = %d %s, want 400", recorder.Code, recorder.Body.String())
	}
}

var _ KnowledgeBaseService = (*patchKnowledgeBaseServiceContract)(nil)

type patchKnowledgeBaseServiceContract struct{}

func (*patchKnowledgeBaseServiceContract) Create(context.Context, service.CreateKnowledgeBaseInput) (*dto.KnowledgeBase, error) {
	return nil, nil
}

func (*patchKnowledgeBaseServiceContract) Get(context.Context, value.ResourceAccess, uuid.UUID) (*dto.KnowledgeBase, error) {
	return nil, nil
}

func (*patchKnowledgeBaseServiceContract) List(context.Context, value.ResourceAccess) ([]*dto.KnowledgeBase, error) {
	return nil, nil
}

func (*patchKnowledgeBaseServiceContract) UpdateBasics(context.Context, service.UpdateKnowledgeBaseBasicsInput) (*dto.KnowledgeBase, error) {
	return nil, nil
}
