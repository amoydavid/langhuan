package pipeline

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// ParseStage parses one immutable File/Web DocumentRevision.
type ParseStage struct {
	revisions        DocumentRevisionRepository
	documents        RevisionDocumentGetter
	rawStore         RawDocumentReader
	parser           parserport.DocumentParser
	maxFileSizeBytes int64
}

// NewParseStage creates a revision-scoped parsing stage.
func NewParseStage(
	revisions DocumentRevisionRepository,
	documents RevisionDocumentGetter,
	rawStore RawDocumentReader,
	parser parserport.DocumentParser,
	maxFileSizeBytes int64,
) ParseStage {
	return ParseStage{
		revisions: revisions, documents: documents, rawStore: rawStore,
		parser: parser, maxFileSizeBytes: maxFileSizeBytes,
	}
}

// Run completes the requested revision without changing Document.active_revision_id.
func (s ParseStage) Run(ctx context.Context, workspaceID, revisionID uuid.UUID) (*model.DocumentRevision, error) {
	revision, err := s.revisions.Get(ctx, workspaceID, revisionID)
	if err != nil {
		return nil, err
	}
	if revision.Status == value.DocumentRevisionReady {
		return revision, nil
	}
	if revision.Kind == value.DocumentKindFAQ {
		return nil, fmt.Errorf("%w: FAQ Revision 不进入通用解析器", domainerrors.ErrValidation)
	}
	if revision.Kind != value.DocumentKindFile && revision.Kind != value.DocumentKindWeb {
		return nil, fmt.Errorf("%w: 不支持解析 Document kind=%q", domainerrors.ErrValidation, revision.Kind)
	}
	document, err := s.documents.Get(ctx, workspaceID, revision.DocumentID)
	if err != nil {
		return nil, err
	}
	if document.KnowledgeBaseID != revision.KnowledgeBaseID || document.Kind != revision.Kind {
		return nil, fmt.Errorf("%w: DocumentRevision lineage 不一致", domainerrors.ErrValidation)
	}
	if s.rawStore == nil {
		return nil, fmt.Errorf("打开原始文档失败: raw store is nil")
	}
	raw, err := s.rawStore.Open(ctx, revision.RawStorageKey)
	if err != nil {
		return nil, fmt.Errorf("打开原始文档失败: %w", err)
	}
	defer raw.Close()
	if s.maxFileSizeBytes <= 0 {
		return nil, fmt.Errorf("%w: max file size must be positive", parserport.ErrParseLimitExceeded)
	}
	content, err := io.ReadAll(io.LimitReader(raw, s.maxFileSizeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取原始文档失败: %w", err)
	}
	if int64(len(content)) > s.maxFileSizeBytes {
		return nil, fmt.Errorf("%w: document exceeds %d bytes", parserport.ErrParseLimitExceeded, s.maxFileSizeBytes)
	}
	parsed, err := s.parser.Parse(ctx, parserport.ParseInput{
		FileType: revision.FileType,
		Title:    document.Title,
		Content:  content,
		Metadata: map[string]any{
			"workspace_id":         workspaceID.String(),
			"knowledge_base_id":    revision.KnowledgeBaseID.String(),
			"document_id":          revision.DocumentID.String(),
			"document_revision_id": revision.ID.String(),
			"title":                document.Title,
			"sha256":               revision.SHA256,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("解析文档失败: %w", err)
	}
	if parsed == nil {
		return nil, fmt.Errorf("解析文档失败: parser returned nil")
	}
	if err := parsed.Manifest.Validate(parsed.Markdown); err != nil {
		return nil, fmt.Errorf("解析文档失败: %w", err)
	}
	if err := s.revisions.CompleteParse(ctx, workspaceID, revision.ID, parsed.Markdown, parsed.Manifest); err != nil {
		return nil, err
	}
	revision.NormalizedMarkdown = parsed.Markdown
	revision.ParseManifest = &parsed.Manifest
	revision.Status = value.DocumentRevisionReady
	revision.ErrorClass = ""
	revision.ErrorMessage = ""
	return revision, nil
}
