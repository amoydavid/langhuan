package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
)

func TestWorkspaceReadinessRecommendedActionPriority(t *testing.T) {
	workspaceID := uuid.New()
	knowledgeBaseID := uuid.New()
	documentID := uuid.New()
	tests := []struct {
		name  string
		facts WorkspaceReadinessFacts
		want  dto.ReadinessAction
	}{
		{name: "no provider", facts: WorkspaceReadinessFacts{}, want: dto.ReadinessConfigureProvider},
		{
			name:  "no selectable embedding model",
			facts: WorkspaceReadinessFacts{HasActiveProvider: true},
			want:  dto.ReadinessCreateEmbeddingModel,
		},
		{
			name:  "no knowledge base",
			facts: WorkspaceReadinessFacts{HasActiveProvider: true, HasSelectableEmbeddingModel: true},
			want:  dto.ReadinessCreateKnowledgeBase,
		},
		{
			name: "no content",
			facts: WorkspaceReadinessFacts{
				HasActiveProvider: true, HasSelectableEmbeddingModel: true, KnowledgeBaseCount: 1,
			},
			want: dto.ReadinessAddContent,
		},
		{
			name: "failed content wins over processing",
			facts: WorkspaceReadinessFacts{
				HasActiveProvider: true, HasSelectableEmbeddingModel: true, KnowledgeBaseCount: 1,
				TotalDocuments: 2, ProcessingDocuments: 1, FailedDocuments: 1,
				RecommendedKnowledgeBaseID: &knowledgeBaseID, RecommendedKnowledgeBaseName: "产品文档",
				RecommendedDocumentID: &documentID, RecommendedDocumentName: "faq-import.csv",
			},
			want: dto.ReadinessResolveFailedDocument,
		},
		{
			name: "only processing content waits",
			facts: WorkspaceReadinessFacts{
				HasActiveProvider: true, HasSelectableEmbeddingModel: true, KnowledgeBaseCount: 1,
				TotalDocuments: 1, ProcessingDocuments: 1,
			},
			want: dto.ReadinessWaitForProcessing,
		},
		{
			name: "ready searchable content suggests retrieval",
			facts: WorkspaceReadinessFacts{
				HasActiveProvider: true, HasSelectableEmbeddingModel: true, KnowledgeBaseCount: 1,
				TotalDocuments: 1, ReadyDocuments: 1, SearchableKnowledgeBaseCount: 1,
			},
			want: dto.ReadinessTestRetrieval,
		},
		{
			name: "ready but not searchable has no action",
			facts: WorkspaceReadinessFacts{
				HasActiveProvider: true, HasSelectableEmbeddingModel: true, KnowledgeBaseCount: 1,
				TotalDocuments: 1, ReadyDocuments: 1,
			},
			want: dto.ReadinessNone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeWorkspaceReadinessStore{facts: &test.facts}
			result, err := NewWorkspaceReadinessService(store).Get(context.Background(), workspaceID)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if result.RecommendedAction != test.want {
				t.Fatalf("RecommendedAction = %q, want %q", result.RecommendedAction, test.want)
			}
			if result.DocumentCounts.Total != test.facts.TotalDocuments ||
				result.DocumentCounts.Ready != test.facts.ReadyDocuments ||
				result.DocumentCounts.Processing != test.facts.ProcessingDocuments ||
				result.DocumentCounts.Failed != test.facts.FailedDocuments {
				t.Fatalf("DocumentCounts = %#v, facts = %#v", result.DocumentCounts, test.facts)
			}
			if store.workspaceID != workspaceID {
				t.Fatalf("store workspaceID = %s, want %s", store.workspaceID, workspaceID)
			}
		})
	}
}

func TestWorkspaceReadinessRejectsNilWorkspaceID(t *testing.T) {
	store := &fakeWorkspaceReadinessStore{}
	if _, err := NewWorkspaceReadinessService(store).Get(context.Background(), uuid.Nil); err == nil {
		t.Fatal("Get() error = nil, want validation error")
	}
	if store.called {
		t.Fatal("store should not be called for nil workspace id")
	}
}

type fakeWorkspaceReadinessStore struct {
	facts       *WorkspaceReadinessFacts
	err         error
	called      bool
	workspaceID uuid.UUID
}

func (s *fakeWorkspaceReadinessStore) GetWorkspaceReadinessFacts(_ context.Context, workspaceID uuid.UUID) (*WorkspaceReadinessFacts, error) {
	s.called = true
	s.workspaceID = workspaceID
	return s.facts, s.err
}
