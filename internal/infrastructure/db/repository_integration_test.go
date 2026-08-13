//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/testsupport"
)

// newAuthTestDB prepares the DB and returns a fresh transaction that the caller
// rolls back, so auth integration tests never pollute one another.
func newAuthTestDB(t *testing.T) (context.Context, *gorm.DB) {
	t.Helper()
	ctx, gormDB := openIntegrationTestDB(t)
	tx := gormDB.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return ctx, tx
}

func openIntegrationTestDB(t *testing.T) (context.Context, *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	databaseURL := testsupport.NewMigratedPostgres(t)
	gormDB, _, err := Open(config.DatabaseConfig{Driver: "postgres", DSN: databaseURL})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return ctx, gormDB
}

// createWorkspaceRow inserts a minimal workspace row for FK constraints and
// returns its ID. Uses an explicit slug to satisfy the NOT NULL column.
func createWorkspaceRow(t *testing.T, ctx context.Context, tx *gorm.DB, slug string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	if err := tx.WithContext(ctx).Create(&WorkspaceRow{
		ID:        id,
		Name:      slug,
		Slug:      slug,
		Metadata:  JSONMap{},
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return id
}
