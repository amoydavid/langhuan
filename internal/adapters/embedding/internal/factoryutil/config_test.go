package factoryutil

import (
	"encoding/json"
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestDecodeStrictRejectsNullAndNonObjectPayloads(t *testing.T) {
	t.Parallel()
	type config struct {
		Timeout int `json:"timeout"`
	}
	for _, raw := range []string{`null`, `[]`, `{"timeout":1} {"timeout":2}`} {
		var target config
		err := DecodeStrict(json.RawMessage(raw), &target, domainerrors.ErrInvalidProviderConfig)
		if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
			t.Fatalf("raw %s error = %v", raw, err)
		}
	}
}
