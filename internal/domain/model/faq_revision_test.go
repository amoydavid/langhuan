package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewFAQRevisionRejectsAnswerOnly(t *testing.T) {
	_, err := NewFAQRevision(NewFAQRevisionInput{
		DocumentRevision: validFAQDocumentRevision(t),
		Answer:           "answer",
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func TestNewFAQRevisionRejectsDuplicateNormalizedQuestions(t *testing.T) {
	_, err := NewFAQRevision(NewFAQRevisionInput{
		DocumentRevision: validFAQDocumentRevision(t),
		Answer:           "answer",
		Questions:        []string{"  How   To Refund? ", "how to refund?"},
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func TestNewFAQRevisionPreservesQuestionOrder(t *testing.T) {
	faq, err := NewFAQRevision(NewFAQRevisionInput{
		DocumentRevision: validFAQDocumentRevision(t),
		Answer:           " answer ",
		Questions:        []string{" first? ", "second?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if faq.Answer != "answer" || len(faq.Questions) != 2 {
		t.Fatalf("faq = %#v", faq)
	}
	if faq.Questions[0].Sequence != 0 || faq.Questions[0].Question != "first?" || faq.Questions[1].Sequence != 1 {
		t.Fatalf("questions = %#v", faq.Questions)
	}
}

func validFAQDocumentRevision(t *testing.T) *DocumentRevision {
	t.Helper()
	revision, err := NewDocumentRevision(NewDocumentRevisionInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
		Kind: value.DocumentKindFAQ, DocumentKind: value.DocumentKindFAQ,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
		ProcessingVersion: 1, Status: value.DocumentRevisionPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return revision
}
