package dto

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestSearchResultCarriesProjectionLineage(t *testing.T) {
	docRev := uuid.New()
	chunkRev := uuid.New()
	generation := uuid.New()
	result := SearchResultFromEvidence(indexport.SearchEvidence{
		DocumentRevisionID: docRev,
		ChunkRevisionID:    chunkRev,
		Content:            "退款将在三个工作日内到账",
	}, generation, 0.031, nil, nil)
	require.Equal(t, docRev, result.DocumentRevisionID)
	require.Equal(t, generation, result.IndexGenerationID)
	require.Equal(t, docRev, result.Citation.DocumentRevisionID)
	require.Equal(t, chunkRev, result.Citation.ChunkRevisionID)
	require.Equal(t, value.CitationStatusValid, result.Citation.Status)
}

func TestEvidenceContentSHA256HashesExactBytes(t *testing.T) {
	content := "第一行\n第二行"
	sum := sha256.Sum256([]byte(content))
	require.Equal(t, hex.EncodeToString(sum[:]), EvidenceContentSHA256(content))
}

func TestCitationContentSHA256MatchesReturnedContent(t *testing.T) {
	result := SearchResultFromEvidence(indexport.SearchEvidence{
		DocumentRevisionID: uuid.New(),
		Content:            "正文内容",
	}, uuid.New(), 0.5, nil, nil)
	require.Equal(t, EvidenceContentSHA256("正文内容"), result.Citation.ContentSHA256)
	require.Len(t, result.Citation.ContentSHA256, 64)
}
