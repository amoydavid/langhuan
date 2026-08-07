//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/service"
)

// TestJinshuManagementAPIIdempotencyE2E covers the three contract capabilities
// added for the jinshu × langhuan knowledge integration:
//  1. Idempotent text ingest (same key+body -> same doc, deduped=true; same key
//     +different body -> 409 idempotency_conflict).
//  2. Bearer job status (bound job ok; unbound job -> 404).
//  3. API key self-introspection (returns scope strings; no key material).
//
// Requires a real PostgreSQL/Redis integration environment.
func TestJinshuManagementAPIIdempotencyE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbID, err := uuid.Parse(env.workspace.Metadata["kb_id"].(string))
	require.NoError(t, err)
	path := "/api/v1/workspaces/" + env.workspace.Slug

	// An unbound KB (not in the API key's binding set) for the 404 assertions.
	var unboundKB struct {
		ID uuid.UUID `json:"id"`
	}
	env.jsonRequest(http.MethodPost, path+"/knowledge-bases", map[string]any{
		"name": "unbound", "embedding_model_id": env.modelID,
	}, http.StatusCreated, &unboundKB)

	// API Key bound to kbID only, with documents:write + documents:read.
	var key struct {
		APIKey string `json:"api_key"`
		Item   struct {
			ID uuid.UUID `json:"id"`
		} `json:"item"`
	}
	env.jsonRequest(http.MethodPost, path+"/api-keys", map[string]any{
		"name": "jinshu idempotency", "knowledge_base_ids": []uuid.UUID{kbID},
		"scopes":     []string{"documents:read", "documents:write"},
		"expiration": map[string]any{"type": "never"},
	}, http.StatusCreated, &key)
	require.NotEmpty(t, key.APIKey)

	// 1. Idempotent text ingest.
	textPath := path + "/knowledge-bases/" + kbID.String() + "/documents/text"
	body := map[string]any{
		"title": "ticket-unique-7788", "content": "# body unique 7788", "content_type": "markdown",
	}
	// First request with Idempotency-Key creates the document + idempotency row.
	var first service.IngestDocumentResult
	bearerRESTWithHeader(t, env, key.APIKey, "Idempotency-Key", "ticket-001", http.MethodPost, textPath, body, http.StatusCreated, &first)
	require.NotNil(t, first.Document)
	require.NotNil(t, first.Job)
	require.False(t, first.Deduped, "first ingest must not be deduped")

	// Same key + same body -> same document/job with deduped=true.
	var replay service.IngestDocumentResult
	bearerRESTWithHeader(t, env, key.APIKey, "Idempotency-Key", "ticket-001", http.MethodPost, textPath, body, http.StatusCreated, &replay)
	require.True(t, replay.Deduped, "replay must be deduped")
	require.Equal(t, first.Document.ID, replay.Document.ID)
	require.Equal(t, first.Job.ID, replay.Job.ID)

	// Same key + different body -> 409 idempotency_conflict.
	bearerRESTWithHeader(t, env, key.APIKey, "Idempotency-Key", "ticket-001", http.MethodPost, textPath, map[string]any{
		"title": "ticket-unique-7788", "content": "# DIFFERENT BODY", "content_type": "markdown",
	}, http.StatusConflict, nil)

	// 2. Bearer job status: bound job ok; unbound job -> 404.
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/jobs/"+first.Job.ID.String(), nil, http.StatusOK, nil)
	// Ingest into the unbound KB via Session to create a job the API key cannot
	// see, then assert 404.
	var unboundResult service.IngestDocumentResult
	env.jsonRequest(http.MethodPost, path+"/knowledge-bases/"+unboundKB.ID.String()+"/documents/text", map[string]any{
		"title": "unbound-doc", "content": "# unbound", "content_type": "markdown",
	}, http.StatusCreated, &unboundResult)
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/jobs/"+unboundResult.Job.ID.String(), nil, http.StatusNotFound, nil)

	// 3. API key self-introspection.
	var self struct {
		Scopes []string `json:"scopes"`
	}
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/api-key/self", nil, http.StatusOK, &self)
	require.ElementsMatch(t, []string{"documents:read", "documents:write"}, self.Scopes)

	// Session caller on api-key/self -> 403.
	env.jsonRequest(http.MethodGet, path+"/api-key/self", nil, http.StatusForbidden, nil)
}

// bearerRESTWithHeader is a small helper for sending a custom header on a
// Bearer request, used for the Idempotency-Key conflict case.
func bearerRESTWithHeader(t *testing.T, env *v030E2E, secret, headerName, headerValue, method, reqPath string, body any, wantStatus int, output any) {
	t.Helper()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, env.server.URL+reqPath, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set(headerName, headerValue)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := env.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("bearerRESTWithHeader %s %s status = %d, want %d", method, reqPath, resp.StatusCode, wantStatus)
	}
	if output != nil && resp.StatusCode < 400 {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(output))
	}
}
