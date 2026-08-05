package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// RerankFailureMode 控制 Rerank 远端失败时的处理策略。
type RerankFailureMode string

const (
	// RerankFailureFallback 在远端可恢复失败时回退到 RRF 顺序，并在结果与日志中标记。
	RerankFailureFallback RerankFailureMode = "fallback"
	// RerankFailureFail 在任意 Rerank 失败时让整个搜索失败。
	RerankFailureFail RerankFailureMode = "fail"
)

// IsValid 判断失败策略是否为已知值。
func (m RerankFailureMode) IsValid() bool {
	return m == RerankFailureFallback || m == RerankFailureFail
}

// ParseRerankFailureMode 把字符串解析为 RerankFailureMode，非法值返回校验错误。
func ParseRerankFailureMode(raw string) (RerankFailureMode, error) {
	mode := RerankFailureMode(raw)
	if !mode.IsValid() {
		return "", fmt.Errorf("%w: 未知重排失败策略 %q", domainerrors.ErrValidation, raw)
	}
	return mode, nil
}

// RankingStage 描述一次检索结果实际使用的排序阶段，供 API 返回与日志记录。
type RankingStage string

const (
	// RankingStageRRF 表示仅使用 RRF 融合排序（Rerank 未启用）。
	RankingStageRRF RankingStage = "rrf"
	// RankingStageRerank 表示成功应用了 Rerank 重排。
	RankingStageRerank RankingStage = "rerank"
	// RankingStageRRFFallback 表示 Rerank 远端失败后回退到 RRF 顺序。
	RankingStageRRFFallback RankingStage = "rrf_fallback"
)

// IsValid 判断排序阶段是否为已知值。
func (s RankingStage) IsValid() bool {
	return s == RankingStageRRF || s == RankingStageRerank || s == RankingStageRRFFallback
}

// Rerank 候选数与失败策略的合法范围。
const (
	MinRerankCandidateTopK = 50
	MaxRerankCandidateTopK = 200
)

// ValidateRerankCandidateTopK 校验候选数是否落在 50..200。
func ValidateRerankCandidateTopK(topK int) error {
	if topK < MinRerankCandidateTopK || topK > MaxRerankCandidateTopK {
		return fmt.Errorf("%w: rerank_candidate_top_k 必须在 %d 到 %d 之间", domainerrors.ErrValidation, MinRerankCandidateTopK, MaxRerankCandidateTopK)
	}
	return nil
}
