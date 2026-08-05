package value

import (
	"errors"
	"reflect"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestChunkingConfigHasNoJSONTags(t *testing.T) {
	typeOfConfig := reflect.TypeOf(ChunkingConfig{})
	for i := 0; i < typeOfConfig.NumField(); i++ {
		field := typeOfConfig.Field(i)
		if tag := field.Tag.Get("json"); tag != "" {
			t.Fatalf("ChunkingConfig.%s has json tag %q", field.Name, tag)
		}
	}
}

func TestChunkingConfigParentChildDefaultsAndValidation(t *testing.T) {
	cfg := DefaultChunkingConfig()
	if cfg.Strategy != ChunkingStrategyAuto || !cfg.EnableParentChild || cfg.ParentChunkSize != 4096 || cfg.ChildChunkSize != 384 {
		t.Fatalf("default config = %#v", cfg)
	}
	cfg.ChildChunkSize = cfg.ParentChunkSize + 1
	if !errors.Is(cfg.Validate(), domainerrors.ErrValidation) {
		t.Fatal("expected invalid child size")
	}
}

func TestChunkingConfigRejectsUnknownStrategy(t *testing.T) {
	cfg := DefaultChunkingConfig()
	cfg.Strategy = ChunkingStrategy("unknown")
	if !errors.Is(cfg.Validate(), domainerrors.ErrValidation) {
		t.Fatal("expected invalid strategy")
	}
}
