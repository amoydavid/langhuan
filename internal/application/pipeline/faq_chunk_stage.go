package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

const faqChunkContractVersion = 1

// FAQRevisionGetter reads one complete FAQ aggregate.
type FAQRevisionGetter interface {
	GetFAQRevision(ctx context.Context, workspaceID, revisionID uuid.UUID) (*model.FAQRevision, error)
}

// FAQChunkStage builds the fixed one-Chunk FAQ representation.
type FAQChunkStage struct {
	faqs      FAQRevisionGetter
	chunkSets ChunkSetRepository
}

// NewFAQChunkStage creates the FAQ-only chunk stage.
func NewFAQChunkStage(faqs FAQRevisionGetter, chunkSets ChunkSetRepository) FAQChunkStage {
	return FAQChunkStage{faqs: faqs, chunkSets: chunkSets}
}

// Build creates or reuses one fixed FAQ ChunkSet without reading a Generation chunking config.
func (s FAQChunkStage) Build(ctx context.Context, workspaceID, revisionID uuid.UUID) (uuid.UUID, error) {
	faq, err := s.faqs.GetFAQRevision(ctx, workspaceID, revisionID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := validateFAQForChunking(faq, workspaceID, revisionID); err != nil {
		return uuid.Nil, err
	}
	documentRevision := faq.DocumentRevision
	config := map[string]any{"contract_version": faqChunkContractVersion}
	candidate := &model.DocumentChunkSet{
		ID: id.New(), WorkspaceID: workspaceID, KnowledgeBaseID: documentRevision.KnowledgeBaseID,
		DocumentID: documentRevision.DocumentID, DocumentRevisionID: documentRevision.ID,
		Strategy: value.ChunkStrategyFAQ, ChunkerVersion: faqChunkContractVersion,
		ChunkingConfig: config, ConfigHash: faqChunkConfigHash(),
		Status: value.ChunkSetBuilding, CreatedAt: time.Now().UTC(),
	}
	chunkSet, err := s.chunkSets.GetOrCreate(ctx, workspaceID, candidate)
	if err != nil {
		return uuid.Nil, err
	}
	if chunkSet.Status == value.ChunkSetReady {
		return chunkSet.ID, nil
	}
	chunk, revision, err := buildFAQChunk(faq, chunkSet.ID)
	if err != nil {
		return uuid.Nil, err
	}
	completed, err := s.chunkSets.Complete(
		ctx, workspaceID, chunkSet.ID,
		[]*model.Chunk{chunk}, []*model.ChunkRevision{revision},
	)
	if err != nil {
		return uuid.Nil, err
	}
	return completed.ID, nil
}

func validateFAQForChunking(faq *model.FAQRevision, workspaceID, revisionID uuid.UUID) error {
	if faq == nil || faq.DocumentRevision == nil || faq.DocumentRevision.ID != revisionID ||
		faq.DocumentRevision.WorkspaceID != workspaceID || faq.DocumentRevision.Kind != value.DocumentKindFAQ ||
		faq.DocumentRevision.Status != value.DocumentRevisionReady || strings.TrimSpace(faq.Answer) == "" ||
		len(faq.Questions) == 0 {
		return fmt.Errorf("%w: FAQ Revision 未就绪或聚合不完整", domainerrors.ErrValidation)
	}
	for index, question := range faq.Questions {
		if question.Sequence != index || strings.TrimSpace(question.Question) == "" {
			return fmt.Errorf("%w: FAQ question sequence/content 无效", domainerrors.ErrValidation)
		}
	}
	return nil
}

func buildFAQChunk(faq *model.FAQRevision, chunkSetID uuid.UUID) (*model.Chunk, *model.ChunkRevision, error) {
	documentRevision := faq.DocumentRevision
	questions := make([]string, len(faq.Questions))
	sourceLines := make([]string, 0, len(faq.Questions)+1)
	for index, question := range faq.Questions {
		questions[index] = question.Question
		sourceLines = append(sourceLines, "Q: "+question.Question)
	}
	sourceLines = append(sourceLines, "A: "+faq.Answer)
	chunkID := id.New()
	revision, err := model.NewChunkRevision(model.NewChunkRevisionInput{
		WorkspaceID: documentRevision.WorkspaceID, KnowledgeBaseID: documentRevision.KnowledgeBaseID,
		DocumentID: documentRevision.DocumentID, DocumentRevisionID: documentRevision.ID,
		ChunkSetID: chunkSetID, ChunkID: chunkID, RevisionNo: 1,
		Content: faq.Answer, EmbeddingContent: strings.Join(questions, "\n"),
		Enabled: true, Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceSystem,
	})
	if err != nil {
		return nil, nil, err
	}
	revisionID := revision.ID
	chunk := &model.Chunk{
		ID: chunkID, WorkspaceID: documentRevision.WorkspaceID, KnowledgeBaseID: documentRevision.KnowledgeBaseID,
		DocumentID: documentRevision.DocumentID, DocumentRevisionID: documentRevision.ID,
		ChunkSetID: chunkSetID, Sequence: 0, SourceContent: strings.Join(sourceLines, "\n"),
		SourceAnchor:     value.SourceAnchor{SourceType: "faq"},
		Metadata:         map[string]any{"question_count": len(questions), "faq_contract_version": faqChunkContractVersion},
		ActiveRevisionID: &revisionID, CreatedAt: time.Now().UTC(),
	}
	return chunk, revision, nil
}

func faqChunkConfigHash() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("faq:%d", faqChunkContractVersion)))
	return hex.EncodeToString(sum[:])
}
