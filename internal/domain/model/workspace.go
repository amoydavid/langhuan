package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// workspaceSlugRegexp 定义合法的 workspace slug：
// 小写字母或数字开头与结尾，中间可含连字符，整体长度 3~64。
// 规格来源：设计规格 §5.2（长度 3–64），字符集同 [a-z0-9-] 且首尾非连字符。
var workspaceSlugRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)

type Workspace struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewWorkspace 创建并校验 workspace。slug 必填且须符合规格正则。
func NewWorkspace(name, slug string, metadata map[string]any) (*Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: workspace 名称不能为空", domainerrors.ErrValidation)
	}

	slug = strings.TrimSpace(slug)
	if !workspaceSlugRegexp.MatchString(slug) {
		return nil, fmt.Errorf("%w: workspace slug 不合法", domainerrors.ErrValidation)
	}

	if metadata == nil {
		metadata = map[string]any{}
	}
	now := time.Now().UTC()
	return &Workspace{
		ID:        uuid.New(),
		Name:      name,
		Slug:      slug,
		Metadata:  metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}
