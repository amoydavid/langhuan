package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// FAQDocument is one complete FAQ revision plus its stable Document identity.
type FAQDocument struct {
	Document  *Document           `json:"document"`
	Revision  *FAQRevisionSummary `json:"revision"`
	Questions []string            `json:"questions"`
	Answer    string              `json:"answer"`
	Job       *Job                `json:"job,omitempty"`
}

// FAQRevisionSummary exposes revision identity without persistence-only fields.
type FAQRevisionSummary struct {
	ID         uuid.UUID                    `json:"id"`
	RevisionNo int64                        `json:"revision_no"`
	Status     value.DocumentRevisionStatus `json:"status"`
	CreatedAt  time.Time                    `json:"created_at"`
}

// FAQDocumentFromModel converts a complete FAQ aggregate to its API form.
func FAQDocumentFromModel(document *model.Document, faq *model.FAQRevision, job *model.Job) *FAQDocument {
	if document == nil || faq == nil || faq.DocumentRevision == nil {
		return nil
	}
	questions := make([]string, len(faq.Questions))
	for index, question := range faq.Questions {
		questions[index] = question.Question
	}
	revision := faq.DocumentRevision
	return &FAQDocument{
		Document: DocumentFromModelWithRevision(document, revision),
		Revision: &FAQRevisionSummary{
			ID: revision.ID, RevisionNo: revision.RevisionNo,
			Status: revision.Status, CreatedAt: revision.CreatedAt,
		},
		Questions: questions, Answer: faq.Answer, Job: JobFromModel(job),
	}
}
