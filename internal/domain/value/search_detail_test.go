package value

import (
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestNormalizeSearchResultDetail(t *testing.T) {
	cases := []struct {
		in   SearchResultDetail
		want SearchResultDetail
	}{
		{"", SearchDetailFull},
		{"full", SearchDetailFull},
		{"lean", SearchDetailLean},
	}
	for _, testCase := range cases {
		if got := NormalizeSearchResultDetail(testCase.in); got != testCase.want {
			t.Fatalf("Normalize(%q) = %q, want %q", testCase.in, got, testCase.want)
		}
	}
}

func TestValidateSearchResultDetail(t *testing.T) {
	for _, valid := range []SearchResultDetail{"", "full", "lean"} {
		if err := ValidateSearchResultDetail(valid); err != nil {
			t.Fatalf("Validate(%q) 不应报错: %v", valid, err)
		}
	}
	for _, invalid := range []SearchResultDetail{"FULL", "fat", "full "} {
		err := ValidateSearchResultDetail(invalid)
		if !errors.Is(err, domainerrors.ErrValidation) {
			t.Fatalf("Validate(%q) 应返回 ErrValidation，实际 %v", invalid, err)
		}
	}
}
