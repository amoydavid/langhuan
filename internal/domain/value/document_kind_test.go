package value

import "testing"

func TestDocumentKindValidate(t *testing.T) {
	for _, kind := range []DocumentKind{DocumentKindFile, DocumentKindFAQ, DocumentKindWeb} {
		if err := kind.Validate(); err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
	}
	if err := DocumentKind("unknown").Validate(); err == nil {
		t.Fatal("unknown document kind unexpectedly valid")
	}
}
