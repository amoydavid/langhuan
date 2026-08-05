package model

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// RerankSnapshot 是 Generation 中保存的可选重排模型不可变快照。
// nil 表示该 Generation 未启用 Rerank。
type RerankSnapshot struct {
	ModelID         uuid.UUID
	ProviderID      uuid.UUID
	ModelName       string
	ModelConfigHash string
	CandidateTopK   int
	FailureMode     value.RerankFailureMode
}

// Validate 校验快照的完整性与合法性。
func (s *RerankSnapshot) Validate() error {
	if s == nil {
		return nil
	}
	if s.ModelID == uuid.Nil || s.ProviderID == uuid.Nil {
		return fmt.Errorf("%w: Rerank 快照 model/provider 不能为空", domainerrors.ErrValidation)
	}
	if strings.TrimSpace(s.ModelName) == "" || strings.TrimSpace(s.ModelConfigHash) == "" {
		return fmt.Errorf("%w: Rerank 快照 model_name/config_hash 不能为空", domainerrors.ErrValidation)
	}
	if err := value.ValidateRerankCandidateTopK(s.CandidateTopK); err != nil {
		return err
	}
	if !s.FailureMode.IsValid() {
		return fmt.Errorf("%w: Rerank 快照 failure_mode 无效", domainerrors.ErrValidation)
	}
	return nil
}

// Clone 返回 RerankSnapshot 的深拷贝；nil 返回 nil。
func (s *RerankSnapshot) Clone() *RerankSnapshot {
	if s == nil {
		return nil
	}
	clone := *s
	return &clone
}
