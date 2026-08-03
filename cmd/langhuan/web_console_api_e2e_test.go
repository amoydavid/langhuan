//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestWebConsoleAPIFlow(t *testing.T) {
	env := startV030E2E(t)
	var workspaceIDs []uuid.UUID
	var userIDs []uuid.UUID
	t.Cleanup(func() {
		if len(workspaceIDs) > 0 {
			if err := env.db.Exec("DELETE FROM workspaces WHERE id IN ?", workspaceIDs).Error; err != nil {
				t.Errorf("cleanup workspaces: %v", err)
			}
		}
		if len(userIDs) > 0 {
			if err := env.db.Exec("DELETE FROM users WHERE id IN ?", userIDs).Error; err != nil {
				t.Errorf("cleanup users: %v", err)
			}
		}
		env.workspace = nil
		env.user = nil
	})

	assertProtocolNamespaces(t, env)

	var bootstrap struct {
		Initialized bool `json:"initialized"`
	}
	doJSON(t, env.client, env.server.URL, http.MethodGet, "/api/v1/auth/bootstrap-status", nil, http.StatusOK, &bootstrap)
	if bootstrap.Initialized {
		t.Fatal("integration database must start without users so bootstrap can be exercised")
	}

	ownerEmail := "console-owner-" + uuid.NewString() + "@example.com"
	var owner dto.AuthenticatedUser
	doJSON(t, env.client, env.server.URL, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email": ownerEmail, "nickname": "管理台所有者", "password": "Passw0rd!",
	}, http.StatusCreated, &owner)
	userIDs = append(userIDs, owner.ID)
	doJSON(t, env.client, env.server.URL, http.MethodGet, "/api/v1/auth/bootstrap-status", nil, http.StatusOK, &bootstrap)
	if !bootstrap.Initialized {
		t.Fatal("bootstrap must be initialized after first-user registration")
	}

	loginResponse := doJSON(t, env.client, env.server.URL, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": ownerEmail, "password": "Passw0rd!",
	}, http.StatusOK, nil)
	assertHTTPOnlySessionCookie(t, loginResponse, env.services.sessionCfg.CookieName)
	env.createPlatformEmbeddingModel()

	firstWorkspace := createConsoleWorkspace(t, env, "管理台 E2E")
	workspaceIDs = append(workspaceIDs, firstWorkspace.ID)
	env.workspace = firstWorkspace
	env.user = &owner
	firstKB := createConsoleKnowledgeBase(t, env.client, env.server.URL, firstWorkspace.Slug, "操作手册", env.modelID)
	firstWorkspace.Metadata = map[string]any{"kb_id": firstKB.ID.String()}

	beforePDF := env.snapshot()
	env.upload("unsupported.pdf", "application/pdf", []byte("%PDF-1.7"), http.StatusUnsupportedMediaType)
	if afterPDF := env.snapshot(); !reflect.DeepEqual(beforePDF, afterPDF) {
		t.Fatalf("PDF upload produced side effects: before=%#v after=%#v", beforePDF, afterPDF)
	}

	created := env.upload("guide.md", "text/markdown", []byte("# 琅嬛\n\n真实管理台接口流程"), http.StatusCreated)
	document := env.waitReady(created.Document.ID)
	job := waitConsoleJobSucceeded(t, env, created.Job.ID)
	if job.DocumentID != document.ID {
		t.Fatalf("job document_id = %s, want %s", job.DocumentID, document.ID)
	}

	memberEmail := "console-member-" + uuid.NewString() + "@example.com"
	var createdInvitation struct {
		ID        uuid.UUID `json:"id"`
		InviteURL string    `json:"invite_url"`
	}
	doJSON(t, env.client, env.server.URL, http.MethodPost, "/api/v1/workspaces/"+firstWorkspace.Slug+"/invitations", map[string]any{
		"invited_email": memberEmail, "role": "member",
	}, http.StatusCreated, &createdInvitation)
	invitationURL, err := url.Parse(createdInvitation.InviteURL)
	if err != nil {
		t.Fatal(err)
	}
	invitationToken := path.Base(invitationURL.Path)
	if invitationToken == "." || invitationToken == "/" || invitationToken == "invitations" {
		t.Fatalf("invalid one-time invite_url: %q", createdInvitation.InviteURL)
	}
	var publicInvitation dto.PublicInvitation
	guestClient := newCookieClient(t)
	doJSON(t, guestClient, env.server.URL, http.MethodGet, "/api/v1/invitations/"+url.PathEscape(invitationToken), nil, http.StatusOK, &publicInvitation)
	if publicInvitation.WorkspaceID != firstWorkspace.ID || publicInvitation.InvitedEmail != memberEmail {
		t.Fatalf("public invitation = %#v", publicInvitation)
	}

	var member dto.AuthenticatedUser
	acceptResponse := doJSON(t, guestClient, env.server.URL, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email": memberEmail, "nickname": "受邀成员", "password": "Passw0rd!", "invitation_token": invitationToken,
	}, http.StatusCreated, &member)
	userIDs = append(userIDs, member.ID)
	assertHTTPOnlySessionCookie(t, acceptResponse, env.services.sessionCfg.CookieName)

	doJSON(t, guestClient, env.server.URL, http.MethodGet, "/api/v1/workspaces/"+firstWorkspace.Slug+"/knowledge-bases/"+firstKB.ID.String(), nil, http.StatusOK, &dto.KnowledgeBase{})
	doJSON(t, guestClient, env.server.URL, http.MethodGet, "/api/v1/workspaces/"+firstWorkspace.Slug+"/documents/"+document.ID.String(), nil, http.StatusOK, &dto.Document{})

	secondWorkspace := createConsoleWorkspace(t, env, "隔离 Workspace")
	workspaceIDs = append(workspaceIDs, secondWorkspace.ID)
	secondKB := createConsoleKnowledgeBase(t, env.client, env.server.URL, secondWorkspace.Slug, "隔离知识库", env.modelID)
	doJSON(t, guestClient, env.server.URL, http.MethodGet, "/api/v1/workspaces/"+secondWorkspace.Slug+"/knowledge-bases/"+secondKB.ID.String(), nil, http.StatusNotFound, nil)

	var members []*dto.Membership
	doJSON(t, env.client, env.server.URL, http.MethodGet, "/api/v1/workspaces/"+firstWorkspace.Slug+"/members", nil, http.StatusOK, &members)
	memberFound := false
	for _, membership := range members {
		if membership.UserID == member.ID {
			memberFound = true
			break
		}
	}
	if !memberFound {
		t.Fatalf("invited user %s not present in members: %#v", member.ID, members)
	}
	var updatedMembership dto.Membership
	doJSON(t, env.client, env.server.URL, http.MethodPatch, "/api/v1/workspaces/"+firstWorkspace.Slug+"/members/"+member.ID.String(), map[string]any{
		"role": "admin",
	}, http.StatusOK, &updatedMembership)
	if updatedMembership.Role != value.RoleAdmin {
		t.Fatalf("updated role = %q, want admin", updatedMembership.Role)
	}

	doJSON(t, env.client, env.server.URL, http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent, nil)
	doJSON(t, env.client, env.server.URL, http.MethodGet, "/api/v1/auth/me", nil, http.StatusUnauthorized, nil)
}

func assertProtocolNamespaces(t *testing.T, env *v030E2E) {
	t.Helper()
	response := doJSON(t, env.client, env.server.URL, http.MethodGet, "/api/v1/unknown", nil, http.StatusNotFound, nil)
	if !strings.Contains(response.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("unknown API content-type = %q", response.Header.Get("Content-Type"))
	}
	var apiError struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeResponse(t, response, &apiError)
	if apiError.Error.Code != "not_found" {
		t.Fatalf("unknown API code = %q", apiError.Error.Code)
	}

	for _, legacyPath := range []string{"/auth/me", "/healthz"} {
		response := doJSON(t, env.client, env.server.URL, http.MethodGet, legacyPath, nil, http.StatusNotFound, nil)
		if strings.Contains(response.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("legacy route %s returned an API response", legacyPath)
		}
	}

	request, err := http.NewRequest(http.MethodGet, env.server.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = env.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		t.Fatal("/mcp fell through to Gin NoRoute")
	}
}

func createConsoleWorkspace(t *testing.T, env *v030E2E, name string) *dto.Workspace {
	t.Helper()
	workspace := &dto.Workspace{}
	doJSON(t, env.client, env.server.URL, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": name, "slug": "console-" + uuid.NewString(),
	}, http.StatusCreated, workspace)
	return workspace
}

func createConsoleKnowledgeBase(t *testing.T, client *http.Client, serverURL, workspaceSlug, name string, modelID uuid.UUID) *dto.KnowledgeBase {
	t.Helper()
	knowledgeBase := &dto.KnowledgeBase{}
	doJSON(t, client, serverURL, http.MethodPost, "/api/v1/workspaces/"+workspaceSlug+"/knowledge-bases", map[string]any{
		"name": name, "embedding_model_id": modelID,
		"chunking_config": map[string]int{"chunk_size": 40, "chunk_overlap": 5},
	}, http.StatusCreated, knowledgeBase)
	return knowledgeBase
}

func waitConsoleJobSucceeded(t *testing.T, env *v030E2E, jobID uuid.UUID) *dto.Job {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		job := &dto.Job{}
		doJSON(t, env.client, env.server.URL, http.MethodGet, "/api/v1/workspaces/"+env.workspace.Slug+"/jobs/"+jobID.String(), nil, http.StatusOK, job)
		switch job.Status {
		case value.JobStatusCompleted:
			return job
		case value.JobStatusFailed, value.JobStatusCancelled:
			t.Fatalf("job %s finished with status %s: %s", job.ID, job.Status, job.ErrorMessage)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job %s did not succeed", jobID)
	return nil
}

func newCookieClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 5 * time.Second}
}

func assertHTTPOnlySessionCookie(t *testing.T, response *http.Response, cookieName string) {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == cookieName {
			if !cookie.HttpOnly {
				t.Fatalf("session cookie %q is not HttpOnly", cookieName)
			}
			return
		}
	}
	t.Fatalf("session cookie %q was not set", cookieName)
}

func doJSON(t *testing.T, client *http.Client, serverURL, method, requestPath string, body any, wantStatus int, output any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, serverURL+requestPath, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(data))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, requestPath, response.StatusCode, wantStatus, data)
	}
	if output != nil && len(data) > 0 {
		if err := json.Unmarshal(data, output); err != nil {
			t.Fatalf("decode %s %s: %v body=%s", method, requestPath, err, data)
		}
	}
	response.Body = io.NopCloser(bytes.NewReader(data))
	return response
}

func decodeResponse(t *testing.T, response *http.Response, output any) {
	t.Helper()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body = io.NopCloser(bytes.NewReader(data))
	if err := json.Unmarshal(data, output); err != nil {
		t.Fatalf("decode response: %v body=%s", err, data)
	}
}
