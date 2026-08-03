//go:build integration

package db

import (
	"os"
	"testing"

	"github.com/dajee/langhuan/internal/infrastructure/migrate"
	"github.com/dajee/langhuan/internal/testsupport"
)

func TestMain(m *testing.M) {
	os.Exit(testsupport.RunPostgresTestMain(m, migrate.Run))
}
