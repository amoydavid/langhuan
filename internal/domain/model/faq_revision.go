package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// FAQRevisionQuestion is one ordered question variant in a FAQ revision.
type FAQRevisionQuestion struct {
	ID                 uuid.UUID
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	Sequence           int
	Question           string
	NormalizedQuestion string
	CreatedAt          time.Time
}

// FAQRevision stores a complete question set and exactly one answer.
type FAQRevision struct {
	DocumentRevision *DocumentRevision
	Answer           string
	Questions        []FAQRevisionQuestion
	CreatedAt        time.Time
}

// NewFAQRevisionInput contains the complete replacement aggregate.
type NewFAQRevisionInput struct {
	DocumentRevision *DocumentRevision
	Answer           string
	Questions        []string
}

// NewFAQRevision validates and normalizes a complete FAQ aggregate.
func NewFAQRevision(input NewFAQRevisionInput) (*FAQRevision, error) {
	if input.DocumentRevision == nil || input.DocumentRevision.Kind != value.DocumentKindFAQ {
		return nil, fmt.Errorf("%w: FAQ 必须关联 FAQ DocumentRevision", domainerrors.ErrValidation)
	}
	answer := strings.TrimSpace(input.Answer)
	if answer == "" || len(input.Questions) == 0 {
		return nil, fmt.Errorf("%w: FAQ 必须包含回答和至少一个问题", domainerrors.ErrValidation)
	}

	createdAt := time.Now().UTC()
	seen := make(map[string]struct{}, len(input.Questions))
	questions := make([]FAQRevisionQuestion, 0, len(input.Questions))
	for sequence, rawQuestion := range input.Questions {
		question := strings.TrimSpace(rawQuestion)
		normalized := normalizeFAQQuestion(question)
		if question == "" || normalized == "" {
			return nil, fmt.Errorf("%w: FAQ 问题不能为空", domainerrors.ErrValidation)
		}
		if _, exists := seen[normalized]; exists {
			return nil, fmt.Errorf("%w: FAQ 问题规范化后不能重复", domainerrors.ErrValidation)
		}
		seen[normalized] = struct{}{}
		questions = append(questions, FAQRevisionQuestion{
			ID: uuid.New(), WorkspaceID: input.DocumentRevision.WorkspaceID,
			KnowledgeBaseID: input.DocumentRevision.KnowledgeBaseID,
			DocumentID:      input.DocumentRevision.DocumentID, DocumentRevisionID: input.DocumentRevision.ID,
			Sequence: sequence, Question: question, NormalizedQuestion: normalized, CreatedAt: createdAt,
		})
	}
	return &FAQRevision{
		DocumentRevision: input.DocumentRevision,
		Answer:           answer, Questions: questions, CreatedAt: createdAt,
	}, nil
}

func normalizeFAQQuestion(question string) string {
	return strings.ToLower(strings.Join(strings.Fields(question), " "))
}
