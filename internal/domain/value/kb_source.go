package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// KbSourceKind 是知识库内容源的种类（kb_sources.kind，spec §3.2）。
// v1.3.0 仅支持 web 网站源；未来飞书源迁入同一实体时在此扩展。
type KbSourceKind string

const (
	// KbSourceKindWeb 是网站内容源（crawl/sitemap/urls 三模式，配置见 §4.1）。
	KbSourceKindWeb KbSourceKind = "web"
)

// Validate 拒绝未知源种类（含空值）。
func (k KbSourceKind) Validate() error {
	switch k {
	case KbSourceKindWeb:
		return nil
	default:
		return fmt.Errorf("%w: 不支持的源种类 %q", domainerrors.ErrValidation, k)
	}
}

// KbSourceStatus 是内容源的启停状态（kb_sources.status）。
type KbSourceStatus string

const (
	// KbSourceStatusActive 表示源参与调度与同步。
	KbSourceStatusActive KbSourceStatus = "active"
	// KbSourceStatusDisabled 表示源被停用：不调度、不入队，已有页面保留。
	KbSourceStatusDisabled KbSourceStatus = "disabled"
)

// Validate 拒绝未知状态（含空值）。
func (s KbSourceStatus) Validate() error {
	switch s {
	case KbSourceStatusActive, KbSourceStatusDisabled:
		return nil
	default:
		return fmt.Errorf("%w: 不支持的源状态 %q", domainerrors.ErrValidation, s)
	}
}
