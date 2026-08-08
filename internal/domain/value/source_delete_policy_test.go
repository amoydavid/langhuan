package value

import (
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestParseSourceDeletePolicyAcceptsKnownValues(t *testing.T) {
	tests := []struct {
		raw  string
		want SourceDeletePolicy
	}{
		{"keep", SourceDeleteKeep},
		{"remove", SourceDeleteRemove},
		// 大小写不敏感、去空白。
		{"  KEEP  ", SourceDeleteKeep},
		{"Remove", SourceDeleteRemove},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := ParseSourceDeletePolicy(tt.raw)
			if err != nil {
				t.Fatalf("ParseSourceDeletePolicy(%q) err = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseSourceDeletePolicy(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseSourceDeletePolicyRejectsInvalidOrEmpty(t *testing.T) {
	tests := []string{
		"",
		"   ",
		"purge",
		"delete",
		"soft-delete",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseSourceDeletePolicy(raw)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("ParseSourceDeletePolicy(%q) err = %v, want ErrValidation", raw, err)
			}
		})
	}
}

func TestSourceDeletePolicyFromConfigLenient(t *testing.T) {
	tests := []struct {
		name string
		raw  any
		want SourceDeletePolicy
	}{
		// 历史/缺失/非法值统一退化为 keep。
		{name: "nil", raw: nil, want: SourceDeleteKeep},
		{name: "empty string", raw: "", want: SourceDeleteKeep},
		{name: "blank string", raw: "   ", want: SourceDeleteKeep},
		{name: "invalid string", raw: "purge", want: SourceDeleteKeep},
		{name: "wrong type int", raw: 42, want: SourceDeleteKeep},
		{name: "wrong type slice", raw: []string{"remove"}, want: SourceDeleteKeep},
		// 合法值按归一化结果返回。
		{name: "keep upper", raw: "KEEP", want: SourceDeleteKeep},
		{name: "remove", raw: "remove", want: SourceDeleteRemove},
		{name: "remove spaced", raw: "  Remove ", want: SourceDeleteRemove},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SourceDeletePolicyFromConfig(tt.raw); got != tt.want {
				t.Fatalf("SourceDeletePolicyFromConfig(%v) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSourceDeletePolicyIsValid(t *testing.T) {
	if !SourceDeleteKeep.IsValid() {
		t.Fatalf("SourceDeleteKeep should be valid")
	}
	if !SourceDeleteRemove.IsValid() {
		t.Fatalf("SourceDeleteRemove should be valid")
	}
	if SourceDeletePolicy("").IsValid() {
		t.Fatalf("empty policy should be invalid")
	}
	if SourceDeletePolicy("purge").IsValid() {
		t.Fatalf("unknown policy should be invalid")
	}
}

func TestSourceDeletePolicyString(t *testing.T) {
	if got, want := SourceDeleteKeep.String(), "keep"; got != want {
		t.Fatalf("keep String() = %q, want %q", got, want)
	}
	if got, want := SourceDeleteRemove.String(), "remove"; got != want {
		t.Fatalf("remove String() = %q, want %q", got, want)
	}
}
