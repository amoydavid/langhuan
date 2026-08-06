package db

import (
	"github.com/dajee/langhuan/internal/domain/model"
)

func sourceConnectionToRow(conn *model.SourceConnection) *SourceConnectionRow {
	return &SourceConnectionRow{
		ID:                    conn.ID,
		WorkspaceID:           conn.WorkspaceID,
		Provider:              conn.Provider,
		Name:                  conn.Name,
		Config:                normalizedJSONMap(conn.Config),
		CredentialsCiphertext: append([]byte(nil), conn.CredentialsCiphertext...),
		Status:                conn.Status,
		CreatedAt:             conn.CreatedAt,
		UpdatedAt:             conn.UpdatedAt,
		DeletedAt:             conn.DeletedAt,
	}
}

func sourceConnectionFromRow(row *SourceConnectionRow) *model.SourceConnection {
	return &model.SourceConnection{
		ID:                    row.ID,
		WorkspaceID:           row.WorkspaceID,
		Provider:              row.Provider,
		Name:                  row.Name,
		Config:                normalizedDomainMap(row.Config),
		CredentialsCiphertext: append([]byte(nil), row.CredentialsCiphertext...),
		Status:                row.Status,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		DeletedAt:             row.DeletedAt,
	}
}
