package http

import (
	"context"
	stdhttp "net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestManualSyncEnqueuesForAdminAndReturnsJobID(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	kbID := uuid.New()
	admin.syncSvc.enqueueResult = &dto.Job{ID: uuid.New(), WorkspaceID: admin.wsID, KnowledgeBaseID: kbID}

	rec := admin.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/sync", nil, "")

	if rec.Code != stdhttp.StatusAccepted {
		t.Fatalf("status/body = %d %s", rec.Code, rec.Body.String())
	}
	if len(admin.syncSvc.enqueueCalls) != 1 {
		t.Fatalf("enqueue calls = %d, want 1", len(admin.syncSvc.enqueueCalls))
	}
	call := admin.syncSvc.enqueueCalls[0]
	if call.WorkspaceID != admin.wsID || call.KnowledgeBaseID != kbID {
		t.Fatalf("enqueue args = %#v", call)
	}
	expected := `{"job_id":"` + admin.syncSvc.enqueueResult.ID.String() + `"}`
	if rec.Body.String() != expected {
		t.Fatalf("body = %q, want %q", rec.Body.String(), expected)
	}
}

func TestManualSyncRejectsMember(t *testing.T) {
	member := newSlugResourceFixtures(t, value.RoleMember, false)
	kbID := uuid.New()

	rec := member.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/sync", nil, "")

	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status/body = %d %s", rec.Code, rec.Body.String())
	}
	if len(member.syncSvc.enqueueCalls) != 0 {
		t.Fatalf("enqueue should not be called for member; calls = %d", len(member.syncSvc.enqueueCalls))
	}
}

func TestManualSyncRejectsBadKBID(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)

	rec := admin.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases/not-a-uuid/sync", nil, "")

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status/body = %d %s", rec.Code, rec.Body.String())
	}
	if len(admin.syncSvc.enqueueCalls) != 0 {
		t.Fatalf("enqueue should not be called for bad id; calls = %d", len(admin.syncSvc.enqueueCalls))
	}
}

// --- fake --------------------------------------------------------------

type fakeKnowledgeBaseSyncService struct {
	enqueueResult *dto.Job
	enqueueErr    error
	enqueueCalls  []manualSyncCall
}

type manualSyncCall struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
}

func (s *fakeKnowledgeBaseSyncService) EnqueueSync(_ context.Context, workspaceID, knowledgeBaseID uuid.UUID) (*dto.Job, error) {
	s.enqueueCalls = append(s.enqueueCalls, manualSyncCall{WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID})
	if s.enqueueErr != nil {
		return nil, s.enqueueErr
	}
	if s.enqueueResult != nil {
		return s.enqueueResult, nil
	}
	return &dto.Job{ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID}, nil
}

var _ KnowledgeBaseSyncService = (*fakeKnowledgeBaseSyncService)(nil)
