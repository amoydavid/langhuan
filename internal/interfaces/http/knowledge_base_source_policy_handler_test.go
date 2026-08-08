package http

import (
	"context"
	stdhttp "net/http"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestSourcePolicyRejectsUnknownValue(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	kbID := uuid.New()

	rec := admin.authedRequest(
		stdhttp.MethodPatch,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/source-policy",
		[]byte(`{"on_delete":"purge"}`),
		"application/json",
	)

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status/body = %d %s, want 400", rec.Code, rec.Body.String())
	}
	if len(admin.sourcePolicySvc.calls) != 0 {
		t.Fatalf("service should not be called on bad value; calls = %d", len(admin.sourcePolicySvc.calls))
	}
}

func TestSourcePolicyRejectsMissingOnDelete(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	kbID := uuid.New()

	rec := admin.authedRequest(
		stdhttp.MethodPatch,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/source-policy",
		[]byte(`{}`),
		"application/json",
	)

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status/body = %d %s, want 400 (strict parser rejects empty)", rec.Code, rec.Body.String())
	}
	if len(admin.sourcePolicySvc.calls) != 0 {
		t.Fatalf("service should not be called on missing on_delete; calls = %d", len(admin.sourcePolicySvc.calls))
	}
}

func TestSourcePolicyRejectsUnknownBodyField(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	kbID := uuid.New()

	rec := admin.authedRequest(
		stdhttp.MethodPatch,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/source-policy",
		[]byte(`{"on_delete":"keep","root_token":"wikcn-leak"}`),
		"application/json",
	)

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status/body = %d %s, want 400 (whole source_config must not be accepted)", rec.Code, rec.Body.String())
	}
	if len(admin.sourcePolicySvc.calls) != 0 {
		t.Fatalf("service should not be called on unknown field; calls = %d", len(admin.sourcePolicySvc.calls))
	}
}

func TestSourcePolicyRejectsMember(t *testing.T) {
	member := newSlugResourceFixtures(t, value.RoleMember, false)
	kbID := uuid.New()

	rec := member.authedRequest(
		stdhttp.MethodPatch,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/source-policy",
		[]byte(`{"on_delete":"remove"}`),
		"application/json",
	)

	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status/body = %d %s, want 403", rec.Code, rec.Body.String())
	}
	if len(member.sourcePolicySvc.calls) != 0 {
		t.Fatalf("service should not be called for member; calls = %d", len(member.sourcePolicySvc.calls))
	}
}

func TestSourcePolicyAcceptsKeepAndRemove(t *testing.T) {
	for _, body := range []string{`{"on_delete":"keep"}`, `{"on_delete":"remove"}`} {
		admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
		kbID := uuid.New()

		rec := admin.authedRequest(
			stdhttp.MethodPatch,
			"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/source-policy",
			[]byte(body),
			"application/json",
		)

		if rec.Code != stdhttp.StatusOK {
			t.Fatalf("body %s status/body = %d %s", body, rec.Code, rec.Body.String())
		}
		if len(admin.sourcePolicySvc.calls) != 1 {
			t.Fatalf("body %s service calls = %d, want 1", body, len(admin.sourcePolicySvc.calls))
		}
		call := admin.sourcePolicySvc.calls[0]
		if call.WorkspaceID != admin.wsID || call.KnowledgeBaseID != kbID {
			t.Fatalf("body %s call args = %#v", body, call)
		}
	}
}

func TestSourcePolicyPassesPolicyToServiceAndReturnsValue(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	kbID := uuid.New()

	rec := admin.authedRequest(
		stdhttp.MethodPatch,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/source-policy",
		[]byte(`{"on_delete":"REMOVE"}`),
		"application/json",
	)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status/body = %d %s", rec.Code, rec.Body.String())
	}
	if len(admin.sourcePolicySvc.calls) != 1 {
		t.Fatalf("service calls = %d, want 1", len(admin.sourcePolicySvc.calls))
	}
	// 严格解析归一化为小写。
	if got := admin.sourcePolicySvc.calls[0].Policy; got != value.SourceDeleteRemove {
		t.Fatalf("policy = %q, want remove", got)
	}
	// 响应回显归一化后的值。
	expected := `{"on_delete":"remove"}`
	if rec.Body.String() != expected {
		t.Fatalf("body = %q, want %q", rec.Body.String(), expected)
	}
}

func TestSourcePolicyRejectsBadKBID(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)

	rec := admin.authedRequest(
		stdhttp.MethodPatch,
		"/api/v1/workspaces/acme/knowledge-bases/not-a-uuid/source-policy",
		[]byte(`{"on_delete":"keep"}`),
		"application/json",
	)

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status/body = %d %s", rec.Code, rec.Body.String())
	}
	if len(admin.sourcePolicySvc.calls) != 0 {
		t.Fatalf("service should not be called for bad id; calls = %d", len(admin.sourcePolicySvc.calls))
	}
}

func TestSourcePolicyMapsNotFoundError(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	kbID := uuid.New()
	admin.sourcePolicySvc.err = domainerrors.ErrNotFound

	rec := admin.authedRequest(
		stdhttp.MethodPatch,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/source-policy",
		[]byte(`{"on_delete":"keep"}`),
		"application/json",
	)

	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("status/body = %d %s, want 404", rec.Code, rec.Body.String())
	}
}

func TestSourcePolicyMapsValidationErrorFromNonFeishuKB(t *testing.T) {
	admin := newSlugResourceFixtures(t, value.RoleAdmin, false)
	kbID := uuid.New()
	// 服务层对非飞书来源返回 ErrValidation（KB 无来源）。
	admin.sourcePolicySvc.err = domainerrors.ErrValidation

	rec := admin.authedRequest(
		stdhttp.MethodPatch,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/source-policy",
		[]byte(`{"on_delete":"keep"}`),
		"application/json",
	)

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status/body = %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// --- fake --------------------------------------------------------------

type sourcePolicyCall struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	Policy          value.SourceDeletePolicy
}

type fakeKnowledgeBaseSourcePolicyService struct {
	calls []sourcePolicyCall
	err   error
}

func (s *fakeKnowledgeBaseSourcePolicyService) UpdateSourceDeletePolicy(_ context.Context, workspaceID, knowledgeBaseID uuid.UUID, policy value.SourceDeletePolicy) error {
	s.calls = append(s.calls, sourcePolicyCall{WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Policy: policy})
	return s.err
}

var _ KnowledgeBaseSourcePolicyService = (*fakeKnowledgeBaseSourcePolicyService)(nil)
