package value

import "testing"

func TestEmbeddingDimensionsAreExactlyIndexedDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dimension int
		valid     bool
	}{
		{dimension: 798, valid: true},
		{dimension: 1024, valid: true},
		{dimension: 2048, valid: true},
		{dimension: 3584, valid: true},
		{dimension: 0, valid: false},
		{dimension: 768, valid: false},
		{dimension: 1536, valid: false},
		{dimension: 4096, valid: false},
	}

	for _, tt := range tests {
		if got := IsSupportedEmbeddingDimension(tt.dimension); got != tt.valid {
			t.Fatalf("dimension %d valid = %v, want %v", tt.dimension, got, tt.valid)
		}
	}
	if DefaultEmbeddingDimension != 1024 {
		t.Fatalf("default dimension = %d, want 1024", DefaultEmbeddingDimension)
	}
}

func TestModelValueObjectsValidateKnownValues(t *testing.T) {
	t.Parallel()

	for _, scope := range []ModelScope{ModelScopePlatform, ModelScopeWorkspace} {
		if !scope.IsValid() {
			t.Fatalf("scope %q should be valid", scope)
		}
	}
	if ModelScope("organization").IsValid() {
		t.Fatal("unknown scope should be invalid")
	}

	for _, status := range []ModelStatus{ModelStatusActive, ModelStatusDisabled} {
		if !status.IsValid() {
			t.Fatalf("status %q should be valid", status)
		}
	}
	if ModelStatus("deleted").IsValid() {
		t.Fatal("unknown status should be invalid")
	}

	for _, modelType := range []ModelType{ModelTypeEmbedding, ModelTypeLLM, ModelTypeRerank} {
		if !modelType.IsValid() {
			t.Fatalf("model type %q should be valid", modelType)
		}
	}
	if ModelType("asr").IsValid() {
		t.Fatal("unknown model type should be invalid")
	}
}
