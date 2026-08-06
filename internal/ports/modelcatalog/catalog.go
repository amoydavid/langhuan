// Package modelcatalog defines the optional Provider model discovery contract.
package modelcatalog

import (
	"context"

	"github.com/dajee/langhuan/internal/domain/value"
)

// Input contains normalized Provider configuration and decrypted credentials for
// one user-triggered catalog request. Callers must clear CredentialsJSON after
// the adapter returns.
type Input struct {
	Scope           value.ModelScope
	Config          map[string]any
	CredentialsJSON []byte
	ModelType       *value.ModelType
	Query           string
}

// Item is one normalized upstream model option. Type and Dimensions may be nil
// when the upstream API does not expose enough metadata for a safe guess.
type Item struct {
	ID          string
	DisplayName string
	Description string
	Type        *value.ModelType
	Dimensions  *int
	Parameters  map[string]any
	Available   bool
}

// Catalog is an optional Provider-side model discovery implementation.
type Catalog interface {
	ListModels(context.Context, Input) ([]Item, error)
}

// CatalogProvider is implemented by a capability Factory that also exposes a
// Provider-wide model catalog. The descriptor builder discovers it without
// forcing every capability Factory to implement model listing.
type CatalogProvider interface {
	ModelCatalog() Catalog
}
