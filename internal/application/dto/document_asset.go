package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

// DocumentAsset 是 document_assets 记录的安全表示，不含对象存储密钥等敏感信息。
type DocumentAsset struct {
	ID              uuid.UUID         `json:"id"`
	DocumentID      uuid.UUID         `json:"document_id"`
	RevisionID      uuid.UUID         `json:"revision_id"`
	OriginalRef     string            `json:"original_ref"`
	PublicURL       string            `json:"public_url"`
	MimeType        string            `json:"mime_type"`
	SHA256          string            `json:"sha256"`
	SizeBytes       int64             `json:"size_bytes"`
	Metadata        map[string]any    `json:"metadata"`
	CreatedAt       time.Time         `json:"created_at"`
}

// DocumentAssetFromModel 把领域 Asset 转为对外 DTO。
// StorageKey 属于对象存储内部标识，不对外暴露。
func DocumentAssetFromModel(asset *model.Asset) DocumentAsset {
	return DocumentAsset{
		ID:          asset.ID,
		DocumentID:  asset.DocumentID,
		RevisionID:  asset.DocumentRevisionID,
		OriginalRef: asset.OriginalRef,
		PublicURL:   asset.PublicURL,
		MimeType:    asset.MimeType,
		SHA256:      asset.SHA256,
		SizeBytes:   asset.SizeBytes,
		Metadata:    asset.Metadata,
		CreatedAt:   asset.CreatedAt,
	}
}
