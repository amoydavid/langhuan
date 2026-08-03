package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type JobRepository interface {
	Get(ctx context.Context, workspaceID uuid.UUID, id uuid.UUID) (*model.Job, error)
}

type JobService struct {
	repo JobRepository
}

func NewJobService(repo JobRepository) *JobService {
	return &JobService{repo: repo}
}

// Get 读取单条 Job。access 用于 API Key 主体把绑定集合下推为 404 边界。
func (s *JobService) Get(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Job, error) {
	job, err := s.repo.Get(ctx, access.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	if !access.Unrestricted && !access.AllowsKnowledgeBase(job.KnowledgeBaseID) {
		return nil, domainerrors.ErrNotFound
	}
	return dto.JobFromModel(job), nil
}
