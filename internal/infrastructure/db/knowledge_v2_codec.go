package db

import (
	"fmt"
	"sort"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func knowledgeBaseV2ToRow(kb *model.KnowledgeBase) *KnowledgeBaseRow {
	return &KnowledgeBaseRow{
		ID: kb.ID, WorkspaceID: kb.WorkspaceID, Name: kb.Name, Description: kb.Description,
		Metadata: normalizedJSONMap(kb.Metadata), ContentVersion: kb.ContentVersion,
		ActiveIndexGenerationID: kb.ActiveIndexGenerationID, FileTreeRootID: kb.FileTreeRootID,
		CreatedAt: kb.CreatedAt, UpdatedAt: kb.UpdatedAt, DeletedAt: kb.DeletedAt,
	}
}

func knowledgeBaseV2FromRow(row *KnowledgeBaseRow) *model.KnowledgeBase {
	return &model.KnowledgeBase{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name, Description: row.Description,
		Metadata: normalizedDomainMap(row.Metadata), ContentVersion: row.ContentVersion,
		ActiveIndexGenerationID: row.ActiveIndexGenerationID, FileTreeRootID: row.FileTreeRootID,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt,
	}
}

func documentV2ToRow(document *model.Document) *DocumentRow {
	return &DocumentRow{
		ID: document.ID, WorkspaceID: document.WorkspaceID, KnowledgeBaseID: document.KnowledgeBaseID,
		Kind: string(document.Kind), Title: document.Title, SourceType: document.SourceType,
		SourceURI: nullableString(document.SourceURI), Status: string(document.Status),
		ActiveRevisionID: document.ActiveRevisionID, Metadata: normalizedJSONMap(document.Metadata),
		CreatedAt: document.CreatedAt, UpdatedAt: document.UpdatedAt, DeletedAt: document.DeletedAt,
	}
}

func documentV2FromRow(row *DocumentRow) *model.Document {
	return &model.Document{
		ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
		Kind: value.DocumentKind(row.Kind), Title: row.Title, SourceType: row.SourceType,
		SourceURI: dereferenceString(row.SourceURI), Status: value.DocumentStatus(row.Status),
		ActiveRevisionID: row.ActiveRevisionID, Metadata: normalizedDomainMap(row.Metadata),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt,
	}
}

func documentRevisionToRow(revision *model.DocumentRevision) (*DocumentRevisionRow, error) {
	var manifest *JSONMap
	if revision.ParseManifest != nil {
		encoded, err := parseManifestToJSONMap(*revision.ParseManifest)
		if err != nil {
			return nil, fmt.Errorf("编码 DocumentRevision parse manifest 失败: %w", err)
		}
		manifest = &encoded
	}
	return &DocumentRevisionRow{
		ID: revision.ID, WorkspaceID: revision.WorkspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
		DocumentID: revision.DocumentID, Kind: string(revision.Kind), RevisionNo: revision.RevisionNo,
		RevisionReason: string(revision.Reason), OriginalFilename: nullableString(revision.OriginalFilename),
		FileType: nullableString(revision.FileType), ContentType: nullableString(revision.ContentType),
		RawStorageKey: nullableString(revision.RawStorageKey), SHA256: nullableString(revision.SHA256),
		SizeBytes: revision.SizeBytes, NormalizedMarkdown: nullableString(revision.NormalizedMarkdown),
		ParseManifest: manifest, ProcessingVersion: revision.ProcessingVersion, Status: string(revision.Status),
		ErrorClass: revision.ErrorClass, ErrorMessage: revision.ErrorMessage, CreatedBy: revision.CreatedBy,
		CreatedAt: revision.CreatedAt, CompletedAt: revision.CompletedAt,
	}, nil
}

func documentRevisionFromRow(row *DocumentRevisionRow) (*model.DocumentRevision, error) {
	var manifest *model.ParseManifest
	if row.ParseManifest != nil {
		decoded, err := parseManifestFromJSONMap(*row.ParseManifest)
		if err != nil {
			return nil, fmt.Errorf("解码 DocumentRevision parse manifest 失败: %w", err)
		}
		manifest = &decoded
	}
	return &model.DocumentRevision{
		ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
		DocumentID: row.DocumentID, Kind: value.DocumentKind(row.Kind), RevisionNo: row.RevisionNo,
		Reason: value.DocumentRevisionReason(row.RevisionReason), OriginalFilename: dereferenceString(row.OriginalFilename),
		FileType: dereferenceString(row.FileType), ContentType: dereferenceString(row.ContentType),
		RawStorageKey: dereferenceString(row.RawStorageKey), SHA256: dereferenceString(row.SHA256),
		SizeBytes: row.SizeBytes, NormalizedMarkdown: dereferenceString(row.NormalizedMarkdown),
		ParseManifest: manifest, ProcessingVersion: row.ProcessingVersion,
		Status: value.DocumentRevisionStatus(row.Status), ErrorClass: row.ErrorClass, ErrorMessage: row.ErrorMessage,
		CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, CompletedAt: row.CompletedAt,
	}, nil
}

func faqRevisionToRows(faq *model.FAQRevision) (*FAQRevisionContentRow, []FAQRevisionQuestionRow) {
	revision := faq.DocumentRevision
	content := &FAQRevisionContentRow{
		DocumentRevisionID: revision.ID, WorkspaceID: revision.WorkspaceID,
		KnowledgeBaseID: revision.KnowledgeBaseID, DocumentID: revision.DocumentID,
		Kind: string(value.DocumentKindFAQ), Answer: faq.Answer, CreatedAt: faq.CreatedAt,
	}
	questions := make([]FAQRevisionQuestionRow, len(faq.Questions))
	for index, question := range faq.Questions {
		questions[index] = FAQRevisionQuestionRow{
			ID: question.ID, WorkspaceID: question.WorkspaceID, KnowledgeBaseID: question.KnowledgeBaseID,
			DocumentID: question.DocumentID, DocumentRevisionID: question.DocumentRevisionID,
			Kind: string(value.DocumentKindFAQ), Sequence: question.Sequence, Question: question.Question,
			NormalizedQuestion: question.NormalizedQuestion, CreatedAt: question.CreatedAt,
		}
	}
	return content, questions
}

func faqRevisionFromRows(revision *model.DocumentRevision, content *FAQRevisionContentRow, rows []FAQRevisionQuestionRow) (*model.FAQRevision, error) {
	if revision == nil || revision.Kind != value.DocumentKindFAQ || content == nil ||
		content.DocumentRevisionID != revision.ID || content.Kind != string(value.DocumentKindFAQ) {
		return nil, fmt.Errorf("%w: FAQ Row lineage 或类型不一致", domainerrors.ErrValidation)
	}
	ordered := append([]FAQRevisionQuestionRow(nil), rows...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Sequence < ordered[j].Sequence })
	questions := make([]model.FAQRevisionQuestion, len(ordered))
	for index, row := range ordered {
		if row.DocumentRevisionID != revision.ID || row.Kind != string(value.DocumentKindFAQ) || row.Sequence != index {
			return nil, fmt.Errorf("%w: FAQ Question Row lineage 或 sequence 无效", domainerrors.ErrValidation)
		}
		questions[index] = model.FAQRevisionQuestion{
			ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
			DocumentID: row.DocumentID, DocumentRevisionID: row.DocumentRevisionID,
			Sequence: row.Sequence, Question: row.Question, NormalizedQuestion: row.NormalizedQuestion,
			CreatedAt: row.CreatedAt,
		}
	}
	return &model.FAQRevision{
		DocumentRevision: revision, Answer: content.Answer, Questions: questions, CreatedAt: content.CreatedAt,
	}, nil
}

func fileTreeNodeToRow(node *model.FileTreeNode) *FileTreeNodeRow {
	var documentKind *string
	if node.NodeType == value.FileTreeNodeFile {
		kind := string(value.DocumentKindFile)
		documentKind = &kind
	}
	return &FileTreeNodeRow{
		ID: node.ID, WorkspaceID: node.WorkspaceID, KnowledgeBaseID: node.KnowledgeBaseID,
		ParentID: node.ParentID, NodeType: string(node.NodeType), Name: node.Name,
		DocumentID: node.DocumentID, DocumentKind: documentKind,
		CreatedAt: node.CreatedAt, UpdatedAt: node.UpdatedAt,
	}
}

func fileTreeNodeFromRow(row *FileTreeNodeRow) *model.FileTreeNode {
	return &model.FileTreeNode{
		ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
		ParentID: row.ParentID, NodeType: value.FileTreeNodeType(row.NodeType), Name: row.Name,
		DocumentID: row.DocumentID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func documentChunkSetToRow(chunkSet *model.DocumentChunkSet) *DocumentChunkSetRow {
	return &DocumentChunkSetRow{
		ID: chunkSet.ID, WorkspaceID: chunkSet.WorkspaceID, KnowledgeBaseID: chunkSet.KnowledgeBaseID,
		DocumentID: chunkSet.DocumentID, DocumentRevisionID: chunkSet.DocumentRevisionID,
		Strategy: string(chunkSet.Strategy), ChunkerVersion: chunkSet.ChunkerVersion,
		ChunkingConfig: normalizedJSONMap(chunkSet.ChunkingConfig), ConfigHash: chunkSet.ConfigHash,
		Status: string(chunkSet.Status), ChunkCount: chunkSet.ChunkCount,
		ErrorClass: chunkSet.ErrorClass, ErrorMessage: chunkSet.ErrorMessage,
		CreatedAt: chunkSet.CreatedAt, ReadyAt: chunkSet.ReadyAt, ArchivedAt: chunkSet.ArchivedAt,
	}
}

func documentChunkSetFromRow(row *DocumentChunkSetRow) *model.DocumentChunkSet {
	return &model.DocumentChunkSet{
		ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
		DocumentID: row.DocumentID, DocumentRevisionID: row.DocumentRevisionID,
		Strategy: value.ChunkStrategy(row.Strategy), ChunkerVersion: row.ChunkerVersion,
		ChunkingConfig: normalizedDomainMap(row.ChunkingConfig), ConfigHash: row.ConfigHash,
		Status: value.ChunkSetStatus(row.Status), ChunkCount: row.ChunkCount,
		ErrorClass: row.ErrorClass, ErrorMessage: row.ErrorMessage,
		CreatedAt: row.CreatedAt, ReadyAt: row.ReadyAt, ArchivedAt: row.ArchivedAt,
	}
}

func chunkV2ToRow(chunk *model.Chunk) (*ChunkRow, error) {
	if err := chunk.SourceAnchor.Validate(); err != nil {
		return nil, fmt.Errorf("编码 Chunk source anchor 失败: %w", err)
	}
	return &ChunkRow{
		ID: chunk.ID, WorkspaceID: chunk.WorkspaceID, KnowledgeBaseID: chunk.KnowledgeBaseID,
		DocumentID: chunk.DocumentID, DocumentRevisionID: chunk.DocumentRevisionID,
		ChunkSetID: chunk.ChunkSetID, Sequence: chunk.Sequence, SourceContent: chunk.SourceContent,
		SourceAnchor: sourceAnchorToJSONMap(chunk.SourceAnchor), Metadata: normalizedJSONMap(chunk.Metadata),
		ActiveRevisionID: chunk.ActiveRevisionID, CreatedAt: chunk.CreatedAt,
	}, nil
}

func chunkV2FromRow(row *ChunkRow) (*model.Chunk, error) {
	anchor, err := sourceAnchorFromJSONMap(row.SourceAnchor)
	if err != nil {
		return nil, fmt.Errorf("解码 Chunk source anchor 失败: %w", err)
	}
	return &model.Chunk{
		ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
		DocumentID: row.DocumentID, DocumentRevisionID: row.DocumentRevisionID,
		ChunkSetID: row.ChunkSetID, Sequence: row.Sequence, SourceContent: row.SourceContent,
		SourceAnchor: anchor, Metadata: normalizedDomainMap(row.Metadata),
		ActiveRevisionID: row.ActiveRevisionID, CreatedAt: row.CreatedAt,
	}, nil
}

func chunkRevisionToRow(revision *model.ChunkRevision) *ChunkRevisionRow {
	return &ChunkRevisionRow{
		ID: revision.ID, WorkspaceID: revision.WorkspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
		DocumentID: revision.DocumentID, DocumentRevisionID: revision.DocumentRevisionID,
		ChunkSetID: revision.ChunkSetID, ChunkID: revision.ChunkID, RevisionNo: revision.RevisionNo,
		BaseRevisionID: revision.BaseRevisionID, Content: revision.Content, ContextHeader: revision.ContextHeader,
		EmbeddingContent: revision.EmbeddingContent, Enabled: revision.Enabled, Status: string(revision.Status),
		EditSource: string(revision.EditSource), EditorUserID: revision.EditorUserID,
		ErrorClass: revision.ErrorClass, ErrorMessage: revision.ErrorMessage,
		CreatedAt: revision.CreatedAt, IndexedAt: revision.IndexedAt,
	}
}

func chunkRevisionFromRow(row *ChunkRevisionRow) *model.ChunkRevision {
	return &model.ChunkRevision{
		ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
		DocumentID: row.DocumentID, DocumentRevisionID: row.DocumentRevisionID,
		ChunkSetID: row.ChunkSetID, ChunkID: row.ChunkID, RevisionNo: row.RevisionNo,
		BaseRevisionID: row.BaseRevisionID, Content: row.Content, ContextHeader: row.ContextHeader,
		EmbeddingContent: row.EmbeddingContent, Enabled: row.Enabled,
		Status: value.ChunkRevisionStatus(row.Status), EditSource: value.ChunkEditSource(row.EditSource),
		EditorUserID: row.EditorUserID, ErrorClass: row.ErrorClass, ErrorMessage: row.ErrorMessage,
		CreatedAt: row.CreatedAt, IndexedAt: row.IndexedAt,
	}
}

func indexGenerationToRow(generation *model.IndexGeneration) *IndexGenerationRow {
	return &IndexGenerationRow{
		ID: generation.ID, WorkspaceID: generation.WorkspaceID, KnowledgeBaseID: generation.KnowledgeBaseID,
		BaseGenerationID: generation.BaseGenerationID, EmbeddingModelID: generation.EmbeddingModelID,
		ProviderID: generation.ProviderID, ModelName: generation.ModelName,
		EmbeddingDimension: generation.EmbeddingDimension, ModelConfigHash: generation.ModelConfigHash,
		ChunkerVersion: generation.ChunkerVersion, ChunkingConfig: normalizedJSONMap(generation.ChunkingConfig),
		RetrievalConfig: normalizedJSONMap(generation.RetrievalConfig), ConfigHash: generation.ConfigHash,
		SourceContentVersion: generation.SourceContentVersion, IndexedContentVersion: generation.IndexedContentVersion,
		Status: string(generation.Status), DocumentCount: generation.DocumentCount, ChunkCount: generation.ChunkCount,
		IndexedCount:    generation.IndexedCount,
		ManualEditCount: generation.ManualEditCount, DisabledChunkCount: generation.DisabledChunkCount,
		ManualEditDisposition: string(generation.ManualEditDisposition),
		ErrorClass:            generation.ErrorClass, ErrorMessage: generation.ErrorMessage,
		CreatedAt: generation.CreatedAt, ReadyAt: generation.ReadyAt,
		ActivatedAt: generation.ActivatedAt, RetiredAt: generation.RetiredAt,
	}
}

func indexGenerationFromRow(row *IndexGenerationRow) *model.IndexGeneration {
	return &model.IndexGeneration{
		ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
		BaseGenerationID: row.BaseGenerationID, EmbeddingModelID: row.EmbeddingModelID,
		ProviderID: row.ProviderID, ModelName: row.ModelName,
		EmbeddingDimension: row.EmbeddingDimension, ModelConfigHash: row.ModelConfigHash,
		ChunkerVersion: row.ChunkerVersion, ChunkingConfig: normalizedDomainMap(row.ChunkingConfig),
		RetrievalConfig: normalizedDomainMap(row.RetrievalConfig), ConfigHash: row.ConfigHash,
		SourceContentVersion: row.SourceContentVersion, IndexedContentVersion: row.IndexedContentVersion,
		Status: value.IndexGenerationStatus(row.Status), DocumentCount: row.DocumentCount, ChunkCount: row.ChunkCount,
		IndexedCount:    row.IndexedCount,
		ManualEditCount: row.ManualEditCount, DisabledChunkCount: row.DisabledChunkCount,
		ManualEditDisposition: value.ManualEditDisposition(row.ManualEditDisposition),
		ErrorClass:            row.ErrorClass, ErrorMessage: row.ErrorMessage,
		CreatedAt: row.CreatedAt, ReadyAt: row.ReadyAt,
		ActivatedAt: row.ActivatedAt, RetiredAt: row.RetiredAt,
	}
}

func retrievalEntryToRow(entry *model.RetrievalEntry) (*RetrievalEntryRow, error) {
	if err := entry.SourceAnchor.Validate(); err != nil {
		return nil, fmt.Errorf("编码 RetrievalEntry source anchor 失败: %w", err)
	}
	return &RetrievalEntryRow{
		ID: entry.ID, WorkspaceID: entry.WorkspaceID, KnowledgeBaseID: entry.KnowledgeBaseID,
		IndexGenerationID: entry.IndexGenerationID, DocumentID: entry.DocumentID,
		DocumentRevisionID: entry.DocumentRevisionID, ChunkSetID: entry.ChunkSetID,
		ChunkID: entry.ChunkID, ChunkRevisionID: entry.ChunkRevisionID, State: string(entry.State),
		SearchContent: entry.SearchContent, Content: entry.Content,
		SourceAnchor: sourceAnchorToJSONMap(entry.SourceAnchor), Metadata: normalizedJSONMap(entry.Metadata),
		FTSDocument: entry.FTSDocument, Embedding: nullableString(entry.Embedding), Dimension: entry.Dimension,
		CreatedAt: entry.CreatedAt, PublishedAt: entry.PublishedAt, RetiredAt: entry.RetiredAt,
	}, nil
}

func retrievalEntryFromRow(row *RetrievalEntryRow) (*model.RetrievalEntry, error) {
	anchor, err := sourceAnchorFromJSONMap(row.SourceAnchor)
	if err != nil {
		return nil, fmt.Errorf("解码 RetrievalEntry source anchor 失败: %w", err)
	}
	return &model.RetrievalEntry{
		ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
		IndexGenerationID: row.IndexGenerationID, DocumentID: row.DocumentID,
		DocumentRevisionID: row.DocumentRevisionID, ChunkSetID: row.ChunkSetID,
		ChunkID: row.ChunkID, ChunkRevisionID: row.ChunkRevisionID,
		State: value.RetrievalEntryState(row.State), SearchContent: row.SearchContent, Content: row.Content,
		SourceAnchor: anchor, Metadata: normalizedDomainMap(row.Metadata), FTSDocument: row.FTSDocument,
		Embedding: dereferenceString(row.Embedding), Dimension: row.Dimension,
		CreatedAt: row.CreatedAt, PublishedAt: row.PublishedAt, RetiredAt: row.RetiredAt,
	}, nil
}

func jobV2ToRow(job *model.Job) *JobRow {
	return &JobRow{
		ID: job.ID, WorkspaceID: job.WorkspaceID, KnowledgeBaseID: job.KnowledgeBaseID,
		DocumentID: nullableUUID(job.DocumentID), DocumentRevisionID: nullableUUID(job.DocumentRevisionID),
		IndexGenerationID: nullableUUID(job.IndexGenerationID), Type: job.Type, Status: string(job.Status),
		Attempts: job.Attempts, ExternalJobID: job.ExternalJobID, Payload: normalizedJSONMap(job.Payload),
		ErrorClass: job.ErrorClass, ErrorMessage: job.ErrorMessage,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
}

func jobV2FromRow(row *JobRow) *model.Job {
	return &model.Job{
		ID: row.ID, WorkspaceID: row.WorkspaceID, KnowledgeBaseID: row.KnowledgeBaseID,
		DocumentID: dereferenceUUID(row.DocumentID), DocumentRevisionID: dereferenceUUID(row.DocumentRevisionID),
		IndexGenerationID: dereferenceUUID(row.IndexGenerationID), Type: row.Type,
		Status: value.JobStatus(row.Status), Attempts: row.Attempts, ExternalJobID: row.ExternalJobID,
		Payload: normalizedDomainMap(row.Payload), ErrorClass: row.ErrorClass, ErrorMessage: row.ErrorMessage,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func normalizedJSONMap(input map[string]any) JSONMap {
	result := make(JSONMap, len(input))
	for key, item := range input {
		result[key] = item
	}
	return result
}

func normalizedDomainMap(input JSONMap) map[string]any {
	result := make(map[string]any, len(input))
	for key, item := range input {
		result[key] = item
	}
	return result
}

func nullableString(input string) *string {
	if input == "" {
		return nil
	}
	value := input
	return &value
}

func dereferenceString(input *string) string {
	if input == nil {
		return ""
	}
	return *input
}

func nullableUUID(input uuid.UUID) *uuid.UUID {
	if input == uuid.Nil {
		return nil
	}
	value := input
	return &value
}

func dereferenceUUID(input *uuid.UUID) uuid.UUID {
	if input == nil {
		return uuid.Nil
	}
	return *input
}
