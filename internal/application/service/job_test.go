package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestJobServiceGetReturnsDTO(t *testing.T) {
	workspaceID := uuid.New()
	job := &model.Job{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: uuid.New(),
		Type: "document_parse_start", Status: value.JobStatusPending,
	}
	service := NewJobService(&fakeJobRepository{job: job})

	got, err := service.Get(context.Background(), value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != job.ID || got.WorkspaceID != workspaceID || got.Type != job.Type {
		t.Fatalf("job DTO = %#v", got)
	}
}

type fakeJobRepository struct {
	job *model.Job
}

func (r *fakeJobRepository) Get(_ context.Context, workspaceID, id uuid.UUID) (*model.Job, error) {
	if r.job == nil || r.job.WorkspaceID != workspaceID || r.job.ID != id {
		return nil, context.Canceled
	}
	return r.job, nil
}
