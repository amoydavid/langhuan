package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestKnowledgeBaseSummarySyncPriority(t *testing.T) {
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	now := time.Date(2026, time.August, 1, 11, 8, 0, 0, time.UTC)
	tests := []struct {
		name  string
		facts KnowledgeBaseSummaryFacts
		want  dto.KnowledgeBaseSyncState
	}{
		{
			name: "candidate ready wins over failures and updates",
			facts: KnowledgeBaseSummaryFacts{
				FailedDocuments: 1, HasUpdatingWork: true,
				CandidateGeneration: &KnowledgeBaseGenerationFacts{ID: uuid.New(), Status: value.IndexGenerationReady, CreatedAt: now},
			},
			want: dto.KnowledgeBaseSyncCandidateReady,
		},
		{
			name:  "failure wins over updating",
			facts: KnowledgeBaseSummaryFacts{FailedDocuments: 1, HasUpdatingWork: true},
			want:  dto.KnowledgeBaseSyncFailed,
		},
		{
			name:  "updating",
			facts: KnowledgeBaseSummaryFacts{HasUpdatingWork: true},
			want:  dto.KnowledgeBaseSyncUpdating,
		},
		{
			name: "synced",
			facts: KnowledgeBaseSummaryFacts{
				ActiveGeneration: &KnowledgeBaseGenerationFacts{ID: uuid.New(), Status: value.IndexGenerationReady, CreatedAt: now},
			},
			want: dto.KnowledgeBaseSyncSynced,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := test.facts
			facts.KnowledgeBaseID = knowledgeBaseID
			facts.KnowledgeBaseName = "产品文档"
			store := &fakeKnowledgeBaseSummaryStore{summary: &facts}
			result, err := NewKnowledgeBaseSummaryService(store).GetSummary(context.Background(), value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}, knowledgeBaseID)
			if err != nil {
				t.Fatalf("GetSummary() error = %v", err)
			}
			if result.SyncState != test.want {
				t.Fatalf("SyncState = %q, want %q", result.SyncState, test.want)
			}
			if result.KnowledgeBaseName != "产品文档" {
				t.Fatalf("KnowledgeBaseName = %q", result.KnowledgeBaseName)
			}
		})
	}
}

func TestKnowledgeBaseSummaryBuildsReadableSafeProjection(t *testing.T) {
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	now := time.Date(2026, time.August, 1, 11, 8, 0, 0, time.UTC)
	activeID, candidateID, documentID, jobID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &fakeKnowledgeBaseSummaryStore{summary: &KnowledgeBaseSummaryFacts{
		KnowledgeBaseID: knowledgeBaseID, KnowledgeBaseName: "产品文档", ContentVersion: 18,
		TotalDocuments: 20, FileDocuments: 14, FAQDocuments: 5, WebDocuments: 1,
		ReadyDocuments: 18, ProcessingDocuments: 1, FailedDocuments: 1,
		ActiveGeneration: &KnowledgeBaseGenerationFacts{
			ID: activeID, Status: value.IndexGenerationReady, ModelDisplayName: "Text Embedding 3 Large",
			EmbeddingDimension: 3584, CreatedAt: now.Add(-time.Hour),
		},
		CandidateGeneration: &KnowledgeBaseGenerationFacts{
			ID: candidateID, Status: value.IndexGenerationReady, ModelDisplayName: "Text Embedding V4",
			EmbeddingDimension: 1024, CreatedAt: now,
		},
		RecentJobs: []KnowledgeBaseJobFacts{{
			ID: jobID, DocumentID: &documentID, Type: "document_parse_start", Status: value.JobStatusFailed,
			TargetType: "document", TargetDisplayName: "installation.md", Attempts: 2,
			ErrorClass: "provider_error", ErrorMessage: "Authorization: Bearer top-secret", CreatedAt: now, UpdatedAt: now,
		}},
		Blockers: []KnowledgeBaseBlockerFacts{{
			Code: "document_processing_failed", ResourceType: "document", ResourceID: documentID,
			ResourceDisplayName: "installation.md",
		}},
	}}

	result, err := NewKnowledgeBaseSummaryService(store).GetSummary(context.Background(), value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}, knowledgeBaseID)
	if err != nil {
		t.Fatalf("GetSummary() error = %v", err)
	}
	if result.ActiveGeneration == nil || !strings.Contains(result.ActiveGeneration.DisplayLabel, "Text Embedding 3 Large") || strings.Contains(result.ActiveGeneration.DisplayLabel, activeID.String()) {
		t.Fatalf("active display label = %#v", result.ActiveGeneration)
	}
	if result.CandidateGeneration == nil || !strings.Contains(result.CandidateGeneration.DisplayLabel, "2026-08-01 11:08") || !strings.Contains(result.CandidateGeneration.DisplayLabel, "待激活") {
		t.Fatalf("candidate display label = %#v", result.CandidateGeneration)
	}
	if len(result.RecentJobs) != 1 || result.RecentJobs[0].ActionLabel != "导入文件" || result.RecentJobs[0].TargetDisplayName != "installation.md" {
		t.Fatalf("recent jobs = %#v", result.RecentJobs)
	}
	if strings.Contains(result.RecentJobs[0].ErrorMessage, "top-secret") || result.RecentJobs[0].ErrorMessage == "" {
		t.Fatalf("unsafe error message = %q", result.RecentJobs[0].ErrorMessage)
	}
	if len(result.Blockers) != 1 || result.Blockers[0].Message == "" || strings.Contains(result.Blockers[0].Message, documentID.String()) {
		t.Fatalf("blockers = %#v", result.Blockers)
	}
}

func TestKnowledgeBaseJobSummaryCursorAndLimit(t *testing.T) {
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	items := []KnowledgeBaseJobFacts{
		{ID: uuid.New(), Type: "document_index", Status: value.JobStatusRunning, TargetType: "document", TargetDisplayName: "退款政策", CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), Type: "chunk_revision_index", Status: value.JobStatusCompleted, TargetType: "document", TargetDisplayName: "installation.md", CreatedAt: now.Add(-time.Minute), UpdatedAt: now},
		{ID: uuid.New(), Type: "index_generation_build", Status: value.JobStatusPending, TargetType: "generation", TargetDisplayName: "2026-08-01 11:08 · Text Embedding V4", CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now},
	}
	store := &fakeKnowledgeBaseSummaryStore{jobs: items}
	service := NewKnowledgeBaseSummaryService(store)

	access := value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}
	first, err := service.ListJobs(context.Background(), access, knowledgeBaseID, JobListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page = %#v", first)
	}
	if store.jobFilter.Limit != 3 || store.jobFilter.BeforeCreatedAt != nil || store.jobFilter.BeforeID != nil {
		t.Fatalf("repository filter = %#v", store.jobFilter)
	}

	store.jobs = nil
	second, err := service.ListJobs(context.Background(), access, knowledgeBaseID, JobListFilter{Limit: 2, Cursor: *first.NextCursor})
	if err != nil {
		t.Fatalf("ListJobs(cursor) error = %v", err)
	}
	if len(second.Items) != 0 || store.jobFilter.BeforeCreatedAt == nil || store.jobFilter.BeforeID == nil || *store.jobFilter.BeforeID != items[1].ID || !store.jobFilter.BeforeCreatedAt.Equal(items[1].CreatedAt) {
		t.Fatalf("decoded cursor filter = %#v, page = %#v", store.jobFilter, second)
	}
}

func TestKnowledgeBaseJobSummaryRejectsInvalidFilter(t *testing.T) {
	service := NewKnowledgeBaseSummaryService(&fakeKnowledgeBaseSummaryStore{})
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	for _, filter := range []JobListFilter{
		{Limit: -1},
		{Limit: 101},
		{Status: value.JobStatus("unknown")},
		{Cursor: "not-a-cursor"},
	} {
		if _, err := service.ListJobs(context.Background(), value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}, knowledgeBaseID, filter); err == nil {
			t.Fatalf("ListJobs(%#v) error = nil", filter)
		}
	}
}

func TestKnowledgeBaseSummaryRejectsUnboundKnowledgeBase(t *testing.T) {
	workspaceID, allowedKB, otherKB := uuid.New(), uuid.New(), uuid.New()
	store := &fakeKnowledgeBaseSummaryStore{}
	access := value.ResourceAccess{WorkspaceID: workspaceID, AllowedKnowledgeBaseIDs: []uuid.UUID{allowedKB}}
	if _, err := NewKnowledgeBaseSummaryService(store).GetSummary(context.Background(), access, otherKB); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("GetSummary() error = %v, want ErrNotFound", err)
	}
	if _, err := NewKnowledgeBaseSummaryService(store).ListJobs(context.Background(), access, otherKB, JobListFilter{}); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("ListJobs() error = %v, want ErrNotFound", err)
	}
}

type fakeKnowledgeBaseSummaryStore struct {
	summary   *KnowledgeBaseSummaryFacts
	jobs      []KnowledgeBaseJobFacts
	err       error
	jobFilter KnowledgeBaseJobFactsFilter
}

func (s *fakeKnowledgeBaseSummaryStore) GetKnowledgeBaseSummaryFacts(_ context.Context, _, _ uuid.UUID) (*KnowledgeBaseSummaryFacts, error) {
	return s.summary, s.err
}

func (s *fakeKnowledgeBaseSummaryStore) ListKnowledgeBaseJobFacts(_ context.Context, _, _ uuid.UUID, filter KnowledgeBaseJobFactsFilter) ([]KnowledgeBaseJobFacts, error) {
	s.jobFilter = filter
	return s.jobs, s.err
}
