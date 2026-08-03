package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

type Workspace struct {
	ID        uuid.UUID      `json:"id"`
	Name      string         `json:"name"`
	Slug      string         `json:"slug"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func WorkspaceFromModel(workspace *model.Workspace) *Workspace {
	if workspace == nil {
		return nil
	}
	return &Workspace{
		ID:        workspace.ID,
		Name:      workspace.Name,
		Slug:      workspace.Slug,
		Metadata:  workspace.Metadata,
		CreatedAt: workspace.CreatedAt,
		UpdatedAt: workspace.UpdatedAt,
	}
}
