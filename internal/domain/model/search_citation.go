package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SearchCitation 是一次检索最终返回结果行的引用遥测（spec §4.2）。
// 只记返回给调用方的行（排序与截断之后），不记中间通道候选；
// 表上不建跨表级联 FK，生命周期由 insights 保留期独立管理（D3）。
type SearchCitation struct {
	ID              uuid.UUID
	WorkspaceID     uuid.UUID
	KbID            uuid.UUID
	SearchRunID     uuid.UUID
	DocumentID      uuid.UUID
	ChunkRevisionID uuid.UUID
	Rank            int
	RetrievalStatus value.RetrievalStatus
	CreatedAt       time.Time
}

// Validate 校验 SearchCitation 的不变量。
func (c *SearchCitation) Validate() error {
	if c == nil {
		return fmt.Errorf("%w: SearchCitation 为空", domainerrors.ErrValidation)
	}
	if c.ID == uuid.Nil || c.WorkspaceID == uuid.Nil || c.KbID == uuid.Nil ||
		c.SearchRunID == uuid.Nil || c.DocumentID == uuid.Nil || c.ChunkRevisionID == uuid.Nil {
		return fmt.Errorf("%w: SearchCitation lineage 为空", domainerrors.ErrValidation)
	}
	if c.Rank < 1 {
		return fmt.Errorf("%w: SearchCitation Rank 必须为正", domainerrors.ErrValidation)
	}
	// citations 只承载有结果行的 run（available/degraded）；empty/failed 无行可记。
	if c.RetrievalStatus != value.RetrievalStatusAvailable &&
		c.RetrievalStatus != value.RetrievalStatusDegraded {
		return fmt.Errorf("%w: SearchCitation RetrievalStatus 只允许 available/degraded", domainerrors.ErrValidation)
	}
	return nil
}
