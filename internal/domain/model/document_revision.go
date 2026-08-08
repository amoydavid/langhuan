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

// NewDocumentRevisionInput contains the complete immutable revision facts.
type NewDocumentRevisionInput struct {
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	Kind               value.DocumentKind
	DocumentKind       value.DocumentKind
	RevisionNo         int64
	Reason             value.DocumentRevisionReason
	OriginalFilename   string
	FileType           string
	ContentType        string
	RawStorageKey      string
	SHA256             string
	SizeBytes          int64
	NormalizedMarkdown string
	ParseManifest      *ParseManifest
	ProcessingVersion  int
	Status             value.DocumentRevisionStatus
	CreatedBy          *uuid.UUID
}

// DocumentRevision stores one immutable acquisition and parse result.
type DocumentRevision struct {
	ID                   uuid.UUID
	WorkspaceID          uuid.UUID
	KnowledgeBaseID      uuid.UUID
	DocumentID           uuid.UUID
	Kind                 value.DocumentKind
	RevisionNo           int64
	Reason               value.DocumentRevisionReason
	OriginalFilename     string
	FileType             string
	ContentType          string
	RawStorageKey        string
	SHA256               string
	SizeBytes            int64
	NormalizedMarkdown   string
	ParseManifest        *ParseManifest
	ParserRawMarkdownKey string
	ProcessingVersion    int
	Status               value.DocumentRevisionStatus
	ErrorClass           string
	ErrorMessage         string
	CreatedBy            *uuid.UUID
	CreatedAt            time.Time
	CompletedAt          *time.Time
}

// NewDocumentRevision validates kind-local facts and creates an immutable revision，
// 内部委托给 NewDocumentRevisionWithID 并自动生成 UUIDv7。
// 保留为兼容入口；显式传入 ID 的场景（如来源同步幂等回写）应使用 NewDocumentRevisionWithID。
func NewDocumentRevision(input NewDocumentRevisionInput) (*DocumentRevision, error) {
	return NewDocumentRevisionWithID(id.New(), input)
}

// NewDocumentRevisionWithID 使用显式 revisionID 创建不可变 revision。
// revisionID 为 uuid.Nil 时返回校验错误，避免调用方误用零值。
func NewDocumentRevisionWithID(revisionID uuid.UUID, input NewDocumentRevisionInput) (*DocumentRevision, error) {
	if revisionID == uuid.Nil {
		return nil, fmt.Errorf("%w: DocumentRevision id 不能为空", domainerrors.ErrValidation)
	}
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || input.DocumentID == uuid.Nil {
		return nil, fmt.Errorf("%w: DocumentRevision lineage 不能为空", domainerrors.ErrValidation)
	}
	if err := input.Kind.Validate(); err != nil {
		return nil, err
	}
	if input.Kind != input.DocumentKind {
		return nil, fmt.Errorf("%w: Document 与 Revision 类型必须一致", domainerrors.ErrValidation)
	}
	if input.RevisionNo < 1 || input.ProcessingVersion < 1 {
		return nil, fmt.Errorf("%w: revision_no 与 processing_version 必须大于 0", domainerrors.ErrValidation)
	}
	if !validDocumentRevisionReason(input.Reason) || !validDocumentRevisionStatus(input.Status) {
		return nil, fmt.Errorf("%w: DocumentRevision reason/status 无效", domainerrors.ErrValidation)
	}
	if input.SizeBytes < 0 {
		return nil, fmt.Errorf("%w: size_bytes 不能为负数", domainerrors.ErrValidation)
	}

	input.OriginalFilename = strings.TrimSpace(input.OriginalFilename)
	input.FileType = strings.TrimSpace(input.FileType)
	input.RawStorageKey = strings.TrimSpace(input.RawStorageKey)
	switch input.Kind {
	case value.DocumentKindFile:
		if input.OriginalFilename == "" || input.FileType == "" || input.RawStorageKey == "" {
			return nil, fmt.Errorf("%w: File Revision 必须包含文件名、file_type 与 raw_storage_key", domainerrors.ErrValidation)
		}
	case value.DocumentKindFAQ:
		if input.OriginalFilename != "" || input.FileType != "" || strings.TrimSpace(input.ContentType) != "" ||
			input.RawStorageKey != "" || strings.TrimSpace(input.SHA256) != "" || input.SizeBytes != 0 ||
			input.NormalizedMarkdown != "" || input.ParseManifest != nil {
			return nil, fmt.Errorf("%w: FAQ Revision 不能包含文件或解析字段", domainerrors.ErrValidation)
		}
	case value.DocumentKindWeb:
		if input.OriginalFilename != "" || input.FileType != "" {
			return nil, fmt.Errorf("%w: Web Revision 不能包含文件名或 file_type", domainerrors.ErrValidation)
		}
	}

	return &DocumentRevision{
		ID: revisionID, WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		DocumentID: input.DocumentID, Kind: input.Kind, RevisionNo: input.RevisionNo,
		Reason: input.Reason, OriginalFilename: input.OriginalFilename, FileType: input.FileType,
		ContentType: strings.TrimSpace(input.ContentType), RawStorageKey: input.RawStorageKey,
		SHA256: strings.TrimSpace(input.SHA256), SizeBytes: input.SizeBytes,
		NormalizedMarkdown: input.NormalizedMarkdown, ParseManifest: input.ParseManifest,
		ProcessingVersion: input.ProcessingVersion, Status: input.Status, CreatedBy: input.CreatedBy,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func validDocumentRevisionReason(reason value.DocumentRevisionReason) bool {
	switch reason {
	case value.DocumentRevisionReasonIngest, value.DocumentRevisionReasonReplace,
		value.DocumentRevisionReasonReparse, value.DocumentRevisionReasonCrawl,
		value.DocumentRevisionReasonEdit:
		return true
	default:
		return false
	}
}

func validDocumentRevisionStatus(status value.DocumentRevisionStatus) bool {
	switch status {
	case value.DocumentRevisionPending, value.DocumentRevisionParsing,
		value.DocumentRevisionReady, value.DocumentRevisionFailed:
		return true
	default:
		return false
	}
}
