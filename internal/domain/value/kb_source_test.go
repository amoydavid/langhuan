package value

import (
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestKbSourceKindValidate(t *testing.T) {
	tests := []struct {
		kind    KbSourceKind
		wantErr bool
	}{
		{kind: KbSourceKindWeb},
		// v1.3.0 仅支持 web：飞书源迁入之前，其它种类一律拒绝。
		{kind: "", wantErr: true},
		{kind: "rss", wantErr: true},
		{kind: "feishu_drive", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			err := tt.kind.Validate()
			if tt.wantErr {
				if !errors.Is(err, domainerrors.ErrValidation) {
					t.Fatalf("Validate(%q) err = %v, want ErrValidation", tt.kind, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate(%q) err = %v, want nil", tt.kind, err)
			}
		})
	}
}

func TestKbSourceStatusValidate(t *testing.T) {
	tests := []struct {
		status  KbSourceStatus
		wantErr bool
	}{
		{status: KbSourceStatusActive},
		{status: KbSourceStatusDisabled},
		{status: "", wantErr: true},
		{status: "paused", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			err := tt.status.Validate()
			if tt.wantErr {
				if !errors.Is(err, domainerrors.ErrValidation) {
					t.Fatalf("Validate(%q) err = %v, want ErrValidation", tt.status, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate(%q) err = %v, want nil", tt.status, err)
			}
		})
	}
}
