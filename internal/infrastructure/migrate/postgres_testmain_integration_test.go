//go:build integration

package migrate

import (
	"os"
	"testing"

	"github.com/dajee/langhuan/internal/testsupport"
)

func TestMain(m *testing.M) {
	os.Exit(testsupport.RunPostgresTestMain(m, nil))
}
