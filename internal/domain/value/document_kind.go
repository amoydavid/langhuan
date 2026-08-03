package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// DocumentKind identifies the immutable business kind of a Document.
type DocumentKind string

const (
	DocumentKindFile DocumentKind = "file"
	DocumentKindFAQ  DocumentKind = "faq"
	DocumentKindWeb  DocumentKind = "web"
)

// Validate rejects unknown document kinds.
func (k DocumentKind) Validate() error {
	switch k {
	case DocumentKindFile, DocumentKindFAQ, DocumentKindWeb:
		return nil
	default:
		return fmt.Errorf("%w: 无效的文档类型 %q", domainerrors.ErrValidation, k)
	}
}
