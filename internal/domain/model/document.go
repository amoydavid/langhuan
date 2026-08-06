package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

const CurrentProcessingVersion = 1

type NewDocumentInput struct {
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	Kind               value.DocumentKind
	Title              string
	FileType           string
	SourceType         string
	Status             value.DocumentStatus
	SHA256             string
	RawStorageKey      string
	SizeBytes          int64
	ContentType        string
	NormalizedMarkdown string
	ProcessingVersion  int
	ParseManifest      ParseManifest
	Metadata           map[string]any
	ErrorMessage       string
	SourceURI          string
	ExternalID         string
}

type Document struct {
	ID                 uuid.UUID
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	Kind               value.DocumentKind
	Title              string
	SourceURI          string
	ActiveRevisionID   *uuid.UUID
	ActiveRevision     *DocumentRevision
	FileType           string
	SourceType         string
	Status             value.DocumentStatus
	SHA256             string
	RawStorageKey      string
	SizeBytes          int64
	ContentType        string
	NormalizedMarkdown string
	ProcessingVersion  int
	ParseManifest      ParseManifest
	Metadata           map[string]any
	FAQQuestionCount   int
	ErrorMessage       string
	ExternalID         string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
}

// NewDocumentIdentity creates stable Document identity without revision-local content fields.
func NewDocumentIdentity(
	workspaceID uuid.UUID,
	knowledgeBaseID uuid.UUID,
	kind value.DocumentKind,
	title string,
	sourceType string,
	sourceURI string,
	metadata map[string]any,
) (*Document, error) {
	if workspaceID == uuid.Nil || knowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: Document lineage 不能为空", domainerrors.ErrValidation)
	}
	if err := kind.Validate(); err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	sourceType = strings.TrimSpace(sourceType)
	if title == "" || sourceType == "" {
		return nil, fmt.Errorf("%w: Document title/source_type 不能为空", domainerrors.ErrValidation)
	}
	if kind == value.DocumentKindWeb {
		normalized, err := value.NormalizeWebSourceURI(sourceURI)
		if err != nil {
			return nil, err
		}
		sourceURI = normalized
	} else if strings.TrimSpace(sourceURI) != "" {
		return nil, fmt.Errorf("%w: 只有 Web Document 可以设置 source_uri", domainerrors.ErrValidation)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	now := time.Now().UTC()
	return &Document{
		ID: id.New(), WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		Kind: kind, Title: title, SourceType: sourceType, SourceURI: sourceURI,
		Status: value.DocumentStatusPending, Metadata: metadata, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func NewDocument(input NewDocumentInput) (*Document, error) {
	if input.KnowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: knowledge_base_id 不能为空", domainerrors.ErrValidation)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: 文档标题不能为空", domainerrors.ErrValidation)
	}
	fileType := strings.TrimSpace(input.FileType)
	if fileType == "" {
		return nil, fmt.Errorf("%w: 文件类型不能为空", domainerrors.ErrValidation)
	}
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		return nil, fmt.Errorf("%w: 来源类型不能为空", domainerrors.ErrValidation)
	}
	if input.Status == "" {
		return nil, fmt.Errorf("%w: 文档状态不能为空", domainerrors.ErrValidation)
	}
	sha256 := strings.TrimSpace(input.SHA256)
	if sha256 == "" {
		return nil, fmt.Errorf("%w: sha256 不能为空", domainerrors.ErrValidation)
	}
	rawStorageKey := strings.TrimSpace(input.RawStorageKey)
	if rawStorageKey == "" {
		return nil, fmt.Errorf("%w: 原始文件存储键不能为空", domainerrors.ErrValidation)
	}
	if input.SizeBytes < 0 {
		return nil, fmt.Errorf("%w: 文件大小不能为负数", domainerrors.ErrValidation)
	}

	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	now := time.Now().UTC()
	kind := input.Kind
	if kind == "" {
		kind = value.DocumentKindFile
	}
	return &Document{
		ID:                 id.New(),
		WorkspaceID:        input.WorkspaceID,
		KnowledgeBaseID:    input.KnowledgeBaseID,
		Kind:               kind,
		Title:              title,
		SourceURI:          input.SourceURI,
		FileType:           fileType,
		SourceType:         sourceType,
		Status:             input.Status,
		SHA256:             sha256,
		RawStorageKey:      rawStorageKey,
		SizeBytes:          input.SizeBytes,
		ContentType:        input.ContentType,
		NormalizedMarkdown: input.NormalizedMarkdown,
		ProcessingVersion:  input.ProcessingVersion,
		ParseManifest:      input.ParseManifest,
		Metadata:           metadata,
		ErrorMessage:       input.ErrorMessage,
		ExternalID:         strings.TrimSpace(input.ExternalID),
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}
