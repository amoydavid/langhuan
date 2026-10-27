package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestSearchCitationValidate(t *testing.T) {
	base := func(mutate func(*SearchCitation)) SearchCitation {
		citation := SearchCitation{
			ID: uuid.New(), WorkspaceID: uuid.New(), KbID: uuid.New(),
			SearchRunID: uuid.New(), DocumentID: uuid.New(), ChunkRevisionID: uuid.New(),
			Rank: 1, RetrievalStatus: value.RetrievalStatusAvailable, CreatedAt: time.Now(),
		}
		if mutate != nil {
			mutate(&citation)
		}
		return citation
	}

	entry0 := base(nil)
	require.NoError(t, entry0.Validate())

	cases := []struct {
		name   string
		mutate func(*SearchCitation)
	}{
		{"rank 零", func(c *SearchCitation) { c.Rank = 0 }},
		{"empty 状态不允许", func(c *SearchCitation) { c.RetrievalStatus = value.RetrievalStatusEmpty }},
		{"failed 状态不允许", func(c *SearchCitation) { c.RetrievalStatus = value.RetrievalStatusFailed }},
		{"document lineage 缺失", func(c *SearchCitation) { c.DocumentID = uuid.Nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := base(tc.mutate)
			require.ErrorIs(t, entry.Validate(), domainerrors.ErrValidation)
		})
	}
}

func TestSearchQueryLogEntryValidate(t *testing.T) {
	base := func(mutate func(*SearchQueryLogEntry)) SearchQueryLogEntry {
		entry := SearchQueryLogEntry{
			ID: uuid.New(), WorkspaceID: uuid.New(), KbID: uuid.New(), SearchRunID: uuid.New(),
			QueryText: "发票抬头怎么改", QueryHash: "h", QueryChars: 7,
			RetrievalStatus: value.RetrievalStatusEmpty, CreatedAt: time.Now(),
		}
		if mutate != nil {
			mutate(&entry)
		}
		return entry
	}

	entry0 := base(nil)
	require.NoError(t, entry0.Validate())

	cases := []struct {
		name   string
		mutate func(*SearchQueryLogEntry)
	}{
		{"空白原文", func(e *SearchQueryLogEntry) { e.QueryText = "   " }},
		{"hash 为空", func(e *SearchQueryLogEntry) { e.QueryHash = "" }},
		{"chars 为负", func(e *SearchQueryLogEntry) { e.QueryChars = -1 }},
		{"lineage 缺失", func(e *SearchQueryLogEntry) { e.KbID = uuid.Nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := base(tc.mutate)
			require.ErrorIs(t, entry.Validate(), domainerrors.ErrValidation)
		})
	}
}
