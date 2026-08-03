package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
	"github.com/dajee/langhuan/internal/ports/storage"
)

const documentParseStartJobType = "document_parse_start"

type DocumentIngestServiceDeps struct {
	Store            DocumentIngestStore
	RawStore         storage.RawDocumentStore
	Queue            queue.JobQueue
	TempDir          string
	AllowedFileTypes []string
}

type DocumentIngestService struct {
	store            DocumentIngestStore
	rawStore         storage.RawDocumentStore
	queue            queue.JobQueue
	tempDir          string
	allowedFileTypes map[string]struct{}
}

type IngestDocumentInput struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	Title           string
	FileName        string
	ContentType     string
	SourceType      string
	Reader          io.Reader
	SizeBytes       int64
	Dedupe          bool
	ParentNodeID    *uuid.UUID
	NodeName        string
}

type IngestDocumentResult struct {
	Document *dto.Document `json:"document"`
	Job      *dto.Job      `json:"job"`
	Deduped  bool          `json:"deduped"`
}

func NewDocumentIngestService(deps DocumentIngestServiceDeps) *DocumentIngestService {
	tempDir := deps.TempDir
	if strings.TrimSpace(tempDir) == "" {
		tempDir = os.TempDir()
	}
	allowedFileTypes := deps.AllowedFileTypes
	if len(allowedFileTypes) == 0 {
		allowedFileTypes = []string{"markdown", "md", "txt", "csv", "xlsx", "docx"}
	}
	allowed := make(map[string]struct{}, len(allowedFileTypes))
	for _, fileType := range allowedFileTypes {
		if canonical := canonicalFileType(fileType); canonical != "" {
			allowed[canonical] = struct{}{}
		}
	}
	return &DocumentIngestService{
		store:            deps.Store,
		rawStore:         deps.RawStore,
		queue:            deps.Queue,
		tempDir:          tempDir,
		allowedFileTypes: allowed,
	}
}

func (s *DocumentIngestService) Ingest(ctx context.Context, input IngestDocumentInput) (*IngestDocumentResult, error) {
	if err := validateIngestDocumentInput(input); err != nil {
		return nil, err
	}
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = "upload"
	}
	fileType := inferFileType(input.FileName, input.ContentType)
	if fileType == "" {
		return nil, fmt.Errorf("%w: 文件类型不能为空", domainerrors.ErrValidation)
	}
	if _, ok := s.allowedFileTypes[fileType]; !ok {
		return nil, domainerrors.ErrUnsupportedFileType
	}
	if s.store == nil {
		return nil, fmt.Errorf("DocumentIngestStore 未配置: %w", domainerrors.ErrValidation)
	}
	return s.ingestV2(ctx, input, sourceType, fileType)
}

func (s *DocumentIngestService) ingestV2(
	ctx context.Context,
	input IngestDocumentInput,
	sourceType string,
	fileType string,
) (*IngestDocumentResult, error) {
	tempPath, hash, actualSize, err := s.copyToTemp(input.Reader)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tempPath)

	var existingDocument *model.Document
	var existingRevision *model.DocumentRevision
	var existingJob *model.Job
	err = s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx DocumentIngestTx) error {
		if _, err := tx.GetKnowledgeBase(txCtx, input.KnowledgeBaseID); err != nil {
			return err
		}
		if !input.Dedupe {
			return nil
		}
		existingDocument, existingRevision, existingJob, err = tx.FindReusableRevision(
			txCtx, input.KnowledgeBaseID, hash, model.CurrentProcessingVersion,
		)
		if errors.Is(err, domainerrors.ErrNotFound) {
			existingDocument, existingRevision, existingJob = nil, nil, nil
			return nil
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if existingDocument != nil {
		return &IngestDocumentResult{
			Document: dto.DocumentFromModelWithRevision(existingDocument, existingRevision),
			Job:      dto.JobFromModel(existingJob), Deduped: true,
		}, nil
	}

	originalFilename := filepath.Base(strings.TrimSpace(input.FileName))
	nodeName := strings.TrimSpace(input.NodeName)
	if nodeName == "" {
		nodeName = originalFilename
	}
	document, err := model.NewDocumentIdentity(
		input.WorkspaceID, input.KnowledgeBaseID, value.DocumentKindFile,
		nodeName, sourceType, "", map[string]any{},
	)
	if err != nil {
		return nil, err
	}
	rawObject, err := s.putRawDocument(ctx, tempPath, storage.RawDocumentInput{
		WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		DocumentID: document.ID, FileName: originalFilename,
		ContentType: strings.TrimSpace(input.ContentType), SizeBytes: actualSize,
	})
	if err != nil {
		return nil, err
	}
	revision, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		DocumentID: document.ID, Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
		OriginalFilename: originalFilename, FileType: fileType,
		ContentType: strings.TrimSpace(input.ContentType), RawStorageKey: rawObject.Key,
		SHA256: hash, SizeBytes: actualSize, ProcessingVersion: model.CurrentProcessingVersion,
		Status: value.DocumentRevisionPending,
	})
	if err != nil {
		return nil, s.deleteRawAfterError(ctx, rawObject.Key, err)
	}
	parentID := input.ParentNodeID
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		Type: documentParseStartJobType, Status: value.JobStatusPending,
		Payload: map[string]any{
			"workspace_id": input.WorkspaceID.String(), "knowledge_base_id": input.KnowledgeBaseID.String(),
			"document_id": document.ID.String(), "document_revision_id": revision.ID.String(),
		},
	})
	if err != nil {
		return nil, s.deleteRawAfterError(ctx, rawObject.Key, err)
	}
	job.Payload["job_id"] = job.ID.String()
	var node *model.FileTreeNode
	var generationID uuid.UUID
	err = s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx DocumentIngestTx) error {
		kb, err := tx.GetKnowledgeBase(txCtx, input.KnowledgeBaseID)
		if err != nil {
			return err
		}
		if kb.ActiveIndexGenerationID == nil || *kb.ActiveIndexGenerationID == uuid.Nil {
			return fmt.Errorf("%w: 知识库缺少 active IndexGeneration", domainerrors.ErrValidation)
		}
		generationID = *kb.ActiveIndexGenerationID
		job.Payload["index_generation_id"] = generationID.String()
		requestedParentID := kb.FileTreeRootID
		if parentID != nil {
			requestedParentID = *parentID
		}
		parent, err := tx.GetFileTreeNodeForUpdate(txCtx, requestedParentID)
		if err != nil {
			return err
		}
		if parent.WorkspaceID != input.WorkspaceID || parent.KnowledgeBaseID != input.KnowledgeBaseID ||
			(parent.NodeType != value.FileTreeNodeRoot && parent.NodeType != value.FileTreeNodeFolder) {
			return domainerrors.ErrNotFound
		}
		documentID := document.ID
		node, err = model.NewFileTreeNode(model.NewFileTreeNodeInput{
			WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
			ParentID: &requestedParentID, NodeType: value.FileTreeNodeFile,
			Name: nodeName, DocumentID: &documentID, DocumentKind: value.DocumentKindFile,
		})
		if err != nil {
			return err
		}
		return tx.CreateFileDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
	})
	if err != nil {
		return nil, s.deleteRawAfterError(ctx, rawObject.Key, err)
	}

	queuePayload, err := json.Marshal(map[string]string{
		"workspace_id": input.WorkspaceID.String(), "knowledge_base_id": input.KnowledgeBaseID.String(),
		"document_id": document.ID.String(), "document_revision_id": revision.ID.String(),
		"generation_id": generationID.String(), "job_id": job.ID.String(),
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.queue.Enqueue(ctx, queue.JobRequest{
		Type: documentParseStartJobType, Payload: queuePayload,
		TaskID: queue.DocumentTaskID(documentParseStartJobType, input.WorkspaceID, revision.ID, generationID),
	}); err != nil {
		cause := fmt.Errorf("入队文档解析任务失败: %w", err)
		if persistErr := s.store.FailCreatedIngest(
			ctx, input.WorkspaceID, document.ID, revision.ID, job.ID,
			"enqueue_error", cause.Error(),
		); persistErr != nil {
			return nil, errors.Join(cause, fmt.Errorf("持久化文档入队失败状态失败: %w", persistErr))
		}
		return nil, cause
	}
	return &IngestDocumentResult{
		Document: dto.DocumentFromModelWithRevision(document, revision), Job: dto.JobFromModel(job), Deduped: false,
	}, nil
}

func validateIngestDocumentInput(input IngestDocumentInput) error {
	if input.WorkspaceID == uuid.Nil {
		return fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	if input.KnowledgeBaseID == uuid.Nil {
		return fmt.Errorf("%w: knowledge_base_id 不能为空", domainerrors.ErrValidation)
	}
	if strings.TrimSpace(input.Title) == "" {
		return fmt.Errorf("%w: 文档标题不能为空", domainerrors.ErrValidation)
	}
	if strings.TrimSpace(input.FileName) == "" {
		return fmt.Errorf("%w: 文件名不能为空", domainerrors.ErrValidation)
	}
	if input.Reader == nil {
		return fmt.Errorf("%w: 文件内容不能为空", domainerrors.ErrValidation)
	}
	if input.SizeBytes < 0 {
		return fmt.Errorf("%w: 文件大小不能为负数", domainerrors.ErrValidation)
	}
	return nil
}

func (s *DocumentIngestService) copyToTemp(reader io.Reader) (string, string, int64, error) {
	if err := os.MkdirAll(s.tempDir, 0o700); err != nil {
		return "", "", 0, fmt.Errorf("创建临时目录失败: %w", err)
	}
	tempFile, err := os.CreateTemp(s.tempDir, "document-ingest-*")
	if err != nil {
		return "", "", 0, fmt.Errorf("创建临时文件失败: %w", err)
	}

	tempPath := tempFile.Name()
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(tempFile, hash), reader)
	closeErr := tempFile.Close()
	if copyErr != nil {
		os.Remove(tempPath)
		return "", "", 0, fmt.Errorf("复制上传文件失败: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tempPath)
		return "", "", 0, fmt.Errorf("关闭临时文件失败: %w", closeErr)
	}

	return tempPath, hex.EncodeToString(hash.Sum(nil)), size, nil
}

func (s *DocumentIngestService) putRawDocument(ctx context.Context, tempPath string, input storage.RawDocumentInput) (*storage.RawDocumentObject, error) {
	file, err := os.Open(tempPath)
	if err != nil {
		return nil, fmt.Errorf("打开临时文件失败: %w", err)
	}
	defer file.Close()

	input.Reader = file
	rawObject, err := s.rawStore.Put(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("保存原始文件失败: %w", err)
	}
	return rawObject, nil
}

func (s *DocumentIngestService) deleteRawAfterError(ctx context.Context, key string, primary error) error {
	if deleteErr := s.rawStore.Delete(ctx, key); deleteErr != nil {
		return fmt.Errorf("%w; 删除原始文件失败: %w", primary, deleteErr)
	}
	return primary
}

func inferFileType(fileName, contentType string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))), ".")
	if ext != "" {
		return canonicalFileType(ext)
	}
	return canonicalFileType(contentType)
}

func canonicalFileType(fileType string) string {
	fileType = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileType)), ".")
	if fileType == "md" || fileType == "markdown" {
		return "markdown"
	}
	return fileType
}
