package providerutil_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dajee/langhuan/internal/adapters/providerutil"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDecodeStrictRejectsNullAndNonObjectPayloads(t *testing.T) {
	t.Parallel()
	type config struct {
		Timeout int `json:"timeout"`
	}
	for _, raw := range []string{`null`, `[]`, `{"timeout":1} {"timeout":2}`} {
		var target config
		err := providerutil.DecodeStrict(json.RawMessage(raw), &target, domainerrors.ErrInvalidProviderConfig)
		if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
			t.Fatalf("raw %s error = %v", raw, err)
		}
	}
}

func TestDecodeStrictRejectsUnknownField(t *testing.T) {
	t.Parallel()
	var target struct {
		Timeout int `json:"timeout"`
	}
	err := providerutil.DecodeStrict(
		json.RawMessage(`{"timeout":30,"secret":"leak"}`),
		&target,
		domainerrors.ErrInvalidProviderConfig,
	)
	if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("error = %v", err)
	}
}

func TestNewHTTPClientRejectsWorkspacePrivateEndpoint(t *testing.T) {
	t.Parallel()
	_, err := providerutil.NewHTTPClient(
		value.ModelScopeWorkspace,
		"https://127.0.0.1:8443",
		30*time.Second,
		nil,
	)
	if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("error = %v", err)
	}
}
