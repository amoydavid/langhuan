package value

import (
	"reflect"
	"testing"
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
