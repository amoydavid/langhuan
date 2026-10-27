package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SearchQueryLogEntry 是 opt-in 的查询原文日志行（spec §4.3）。
// 仅 knowledge_bases.query_log_enabled=true 的库由投递侧构造写入（门控在构造时，
// 非事后过滤）；任何日志、错误输出与指标不得包含 QueryText。
type SearchQueryLogEntry struct {
	ID              uuid.UUID
	WorkspaceID     uuid.UUID
	KbID            uuid.UUID
	SearchRunID     uuid.UUID
	QueryText       string
	QueryHash       string
	QueryChars      int
	RetrievalStatus value.RetrievalStatus
	CreatedAt       time.Time
}

// Validate 校验 SearchQueryLogEntry 的不变量。
func (e *SearchQueryLogEntry) Validate() error {
	if e == nil {
		return fmt.Errorf("%w: SearchQueryLogEntry 为空", domainerrors.ErrValidation)
	}
	if e.ID == uuid.Nil || e.WorkspaceID == uuid.Nil || e.KbID == uuid.Nil || e.SearchRunID == uuid.Nil {
		return fmt.Errorf("%w: SearchQueryLogEntry lineage 为空", domainerrors.ErrValidation)
	}
	if strings.TrimSpace(e.QueryText) == "" {
		return fmt.Errorf("%w: SearchQueryLogEntry QueryText 不能为空白", domainerrors.ErrValidation)
	}
	if e.QueryHash == "" {
		return fmt.Errorf("%w: SearchQueryLogEntry QueryHash 为空", domainerrors.ErrValidation)
	}
	if e.QueryChars < 0 {
		return fmt.Errorf("%w: SearchQueryLogEntry QueryChars 为负", domainerrors.ErrValidation)
	}
	if err := e.RetrievalStatus.Validate(); err != nil {
		return err
	}
	return nil
}
