package http

import (
	"context"
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

var _ KnowledgeBaseService = (*patchKnowledgeBaseServiceContract)(nil)

type patchKnowledgeBaseServiceContract struct{}

func (*patchKnowledgeBaseServiceContract) Create(context.Context, service.CreateKnowledgeBaseInput) (*dto.KnowledgeBase, error) {
	return nil, nil
}

func (*patchKnowledgeBaseServiceContract) Get(context.Context, uuid.UUID, uuid.UUID) (*dto.KnowledgeBase, error) {
	return nil, nil
}

func (*patchKnowledgeBaseServiceContract) List(context.Context, uuid.UUID) ([]*dto.KnowledgeBase, error) {
	return nil, nil
}

func (*patchKnowledgeBaseServiceContract) UpdateBasics(context.Context, service.UpdateKnowledgeBaseBasicsInput) (*dto.KnowledgeBase, error) {
	return nil, nil
}
