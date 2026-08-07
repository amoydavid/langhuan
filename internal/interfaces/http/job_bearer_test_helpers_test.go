package http

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// errNotFoundSentinel is the sentinel used by job service fakes to signal a
// 404 boundary (the JobQueryService contract maps ErrNotFound -> 404).
var errNotFoundSentinel = domainerrors.ErrNotFound

// jobServiceFake is a minimal JobQueryService for handler tests.
type jobServiceFake struct {
	get func(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Job, error)
}

func (f *jobServiceFake) Get(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Job, error) {
	if f.get != nil {
		return f.get(ctx, access, id)
	}
	return nil, nil
}
