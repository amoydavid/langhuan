package dto

import (
	"time"

	"github.com/dajee/langhuan/internal/domain/value"
)

// ModelCatalogItem is a safe, non-persistent option returned by a Provider API.
type ModelCatalogItem struct {
	ID          string           `json:"id"`
	DisplayName string           `json:"display_name"`
	Description string           `json:"description"`
	Type        *value.ModelType `json:"type"`
	Dimensions  *int             `json:"dimensions"`
	Parameters  map[string]any   `json:"parameters"`
	Available   bool             `json:"available"`
}

// ModelCatalogResponse is the result of one user-triggered Provider catalog fetch.
type ModelCatalogResponse struct {
	Provider  string             `json:"provider"`
	Items     []ModelCatalogItem `json:"items"`
	Source    string             `json:"source"`
	FetchedAt time.Time          `json:"fetched_at"`
}
