//go:build integration

package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestWorkspaceTxRunnerSetsTenantLocalContextWithoutLeaking(t *testing.T) {
	t.Parallel()

	ctx, database := openIntegrationTestDB(t)
	workspaceID := uuid.New()
	wantRollback := errors.New("rollback workspace transaction")
	runner := NewWorkspaceTxRunner(database)

	err := runner.WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var got sql.NullString
		if err := tx.WithContext(ctx).
			Raw("SELECT current_setting('app.workspace_id', true)").
			Scan(&got).Error; err != nil {
			return err
		}
		if !got.Valid || got.String != workspaceID.String() {
			t.Fatalf("app.workspace_id = %#v, want %s", got, workspaceID)
		}
		return wantRollback
	})
	if !errors.Is(err, wantRollback) {
		t.Fatalf("WithinWorkspace() error = %v, want rollback sentinel", err)
	}

	var leaked sql.NullString
	if err := database.WithContext(context.Background()).
		Raw("SELECT current_setting('app.workspace_id', true)").
		Scan(&leaked).Error; err != nil {
		t.Fatal(err)
	}
	if leaked.Valid && leaked.String != "" {
		t.Fatalf("app.workspace_id leaked outside transaction: %#v", leaked)
	}
}

func TestWorkspaceTxRunnerRejectsEmptyWorkspaceID(t *testing.T) {
	t.Parallel()

	_, database := openIntegrationTestDB(t)
	runner := NewWorkspaceTxRunner(database)
	called := false
	err := runner.WithinWorkspace(context.Background(), uuid.Nil, func(*gorm.DB) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("WithinWorkspace() error = nil, want validation error")
	}
	if called {
		t.Fatal("WithinWorkspace() called callback for empty workspace ID")
	}
}
