//go:build integration

package testsupport

import (
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

func TestPostgresServerConfigFromEnv(t *testing.T) {
	tests := []struct {
		name       string
		values     map[string]string
		wantExtern bool
		wantErr    string
	}{
		{name: "automatic fallback"},
		{
			name: "dsn without run id",
			values: map[string]string{
				postgresDSNEnv: "postgres://langhuan:langhuan@127.0.0.1:5432/langhuan_test?sslmode=disable",
			},
			wantErr: "必须同时设置",
		},
		{
			name: "run id without dsn",
			values: map[string]string{
				postgresRunIDEnv: "integration-run",
			},
			wantErr: "必须同时设置",
		},
		{
			name: "reject development database",
			values: map[string]string{
				postgresDSNEnv:   "postgres://langhuan:langhuan@127.0.0.1:5432/langhuan?sslmode=disable",
				postgresRunIDEnv: "integration-run",
			},
			wantErr: "测试数据库名必须以 langhuan_test 开头",
		},
		{
			name: "external disposable server",
			values: map[string]string{
				postgresDSNEnv:   "postgres://langhuan:langhuan@127.0.0.1:5432/langhuan_test?sslmode=disable",
				postgresRunIDEnv: "integration-run",
			},
			wantExtern: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := postgresServerConfigFromEnv(func(key string) string {
				return test.values[key]
			})
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("postgresServerConfigFromEnv() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.external != test.wantExtern {
				t.Fatalf("external = %t, want %t", config.external, test.wantExtern)
			}
		})
	}
}

func TestValidateTestPostgresImage(t *testing.T) {
	if err := validateTestPostgresImage([]testcontainers.ImageInfo{{Name: testPostgresImage}}); err != nil {
		t.Fatalf("validateTestPostgresImage() error = %v", err)
	}

	err := validateTestPostgresImage([]testcontainers.ImageInfo{{Name: "pgvector/pgvector:pg17"}})
	if err == nil {
		t.Fatal("validateTestPostgresImage() error = nil, want missing image error")
	}
	for _, expected := range []string{testPostgresImage, "make test-image"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("validateTestPostgresImage() error = %v, want containing %q", err, expected)
		}
	}
}
