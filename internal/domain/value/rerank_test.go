package value_test

import (
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestRerankFailureModeIsValid(t *testing.T) {
	t.Parallel()
	for _, mode := range []value.RerankFailureMode{value.RerankFailureFallback, value.RerankFailureFail} {
		if !mode.IsValid() {
			t.Fatalf("%q should be valid", mode)
		}
	}
	if value.RerankFailureMode("unknown").IsValid() {
		t.Fatal("unknown failure mode should be invalid")
	}
}

func TestParseRerankFailureMode(t *testing.T) {
	t.Parallel()
	got, err := value.ParseRerankFailureMode("fallback")
	if err != nil || got != value.RerankFailureFallback {
		t.Fatalf("parse fallback = %q err = %v", got, err)
	}
	_, err = value.ParseRerankFailureMode("nope")
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("invalid parse err = %v", err)
	}
}

func TestRankingStageIsValid(t *testing.T) {
	t.Parallel()
	for _, stage := range []value.RankingStage{value.RankingStageRRF, value.RankingStageRerank, value.RankingStageRRFFallback} {
		if !stage.IsValid() {
			t.Fatalf("%q should be valid", stage)
		}
	}
	if value.RankingStage("unknown").IsValid() {
		t.Fatal("unknown stage should be invalid")
	}
}

func TestValidateRerankCandidateTopK(t *testing.T) {
	t.Parallel()
	for _, topK := range []int{50, 100, 200} {
		if err := value.ValidateRerankCandidateTopK(topK); err != nil {
			t.Fatalf("topK %d err = %v", topK, err)
		}
	}
	for _, topK := range []int{49, 201, 0} {
		if err := value.ValidateRerankCandidateTopK(topK); !errors.Is(err, domainerrors.ErrValidation) {
			t.Fatalf("topK %d err = %v", topK, err)
		}
	}
}
