package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/value"
)

func TestWorkspaceAPIKeyCarriesDomainFactsWithoutSerializationTags(t *testing.T) {
	expires := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	key := WorkspaceAPIKey{
		ID:               uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		WorkspaceID:      uuid.MustParse("00000000-0000-4000-8000-000000000002"),
		Name:             "检索 Agent",
		TokenHash:        "deadbeef",
		TokenPrefix:      "lhk_a1b2c3d4",
		Scopes:           []value.APIScope{value.ScopeSearchRead, value.ScopeDocumentsRead},
		KnowledgeBaseIDs: []uuid.UUID{uuid.MustParse("00000000-0000-4000-8000-000000000003")},
		ExpiresAt:        &expires,
	}
	require.Equal(t, "检索 Agent", key.Name)
	require.Len(t, key.Scopes, 2)
	require.Len(t, key.KnowledgeBaseIDs, 1)
	// ExpiresAt=nil 表示不限期。
	never := WorkspaceAPIKey{}
	require.Nil(t, never.ExpiresAt)
}
