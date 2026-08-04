package pipeline

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
	portstorage "github.com/dajee/langhuan/internal/ports/storage"
)

// fakeAssetRepositoryForPipeline 实现 pipeline.AssetRepository 接口，记录调用用于断言。
type fakeAssetRepositoryForPipeline struct {
	createdAssets []model.Asset
	deletedRevID  uuid.UUID
	createCalls   int
	deleteCalls   int
}

func (r *fakeAssetRepositoryForPipeline) DeleteAssetsByRevision(_ context.Context, _, revisionID uuid.UUID) error {
	r.deleteCalls++
	r.deletedRevID = revisionID
	return nil
}

func (r *fakeAssetRepositoryForPipeline) CreateAssets(_ context.Context, assets []model.Asset) error {
	r.createCalls++
	r.createdAssets = append(r.createdAssets, assets...)
	return nil
}

// fakeAssetStoreForPipeline 是简单 AssetStore 实现。
type fakeAssetStoreForPipeline struct {
	putCalls int
}

func (s *fakeAssetStoreForPipeline) Put(_ context.Context, object portstorage.ObjectInput) (*portstorage.StoredObject, error) {
	s.putCalls++
	return &portstorage.StoredObject{
		Key:       object.Key,
		PublicURL: "https://cdn.example.com/" + object.Key,
		SizeBytes: int64(len(object.Data)),
	}, nil
}

func (s *fakeAssetStoreForPipeline) Delete(_ context.Context, _ string) error {
	return nil
}

func TestCompleteAsyncParsePersistsAssets(t *testing.T) {
	ctx := context.Background()
	workspaceID := uuid.New()
	revisionID := uuid.New()

	revision := &model.DocumentRevision{
		ID:          revisionID,
		WorkspaceID: workspaceID,
		Status:      value.DocumentRevisionPending,
		Kind:        value.DocumentKindFile,
	}

	repo := &fakeRevisionRepository{revision: revision}
	assetRepo := &fakeAssetRepositoryForPipeline{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake png"))
	}))
	defer server.Close()

	cfg := config.AssetsStorageConfig{
		MaxCountPerDocument: 500,
		MaxImageSizeBytes:   10 * 1024 * 1024,
		AllowedMimeTypes:    []string{"image/png"},
	}
	documentID := uuid.New()
	assetResolver := NewAssetResolver(&fakeAssetStoreForPipeline{}, server.Client(), cfg, workspaceID, documentID, revisionID)

	p := &DocumentPipeline{
		revisions: repo,
		assets:    assetRepo,
	}

	markdown := "# 标题\n\n![image](" + server.URL + "/test.png)\n\n正文"
	parsed := &parserport.ParsedDocument{
		Markdown: markdown,
		Manifest: model.ParseManifest{
			Version:       model.CurrentParseManifestVersion,
			Parser:        "pdf",
			ParserVersion: 1,
			Blocks: []model.ParsedBlock{
				{Sequence: 0, Kind: model.BlockKindParagraph, NormalizedStart: 0, NormalizedEnd: len(markdown)},
			},
		},
	}

	err := p.CompleteAsyncParse(ctx, workspaceID, revisionID, parsed, assetResolver)
	if err != nil {
		t.Fatalf("CompleteAsyncParse() error = %v", err)
	}

	// 验证资产被持久化
	if assetRepo.createCalls != 1 {
		t.Fatalf("CreateAssets calls = %d, want 1", assetRepo.createCalls)
	}
	if len(assetRepo.createdAssets) != 1 {
		t.Fatalf("created assets = %d, want 1", len(assetRepo.createdAssets))
	}
	asset := assetRepo.createdAssets[0]
	if asset.WorkspaceID != workspaceID {
		t.Fatalf("asset WorkspaceID = %v, want %v", asset.WorkspaceID, workspaceID)
	}
	if asset.DocumentID != documentID {
		t.Fatalf("asset DocumentID = %v, want %v", asset.DocumentID, documentID)
	}
	if asset.DocumentRevisionID != revisionID {
		t.Fatalf("asset DocumentRevisionID = %v, want %v", asset.DocumentRevisionID, revisionID)
	}
	if asset.SHA256 == "" {
		t.Fatal("asset SHA256 is empty")
	}
	if asset.MimeType != "image/png" {
		t.Fatalf("asset MimeType = %q, want image/png", asset.MimeType)
	}

	// 验证清理旧资产被调用（幂等）
	if assetRepo.deleteCalls != 1 {
		t.Fatalf("DeleteAssetsByRevision calls = %d, want 1", assetRepo.deleteCalls)
	}
	if assetRepo.deletedRevID != revisionID {
		t.Fatalf("deleted revision ID = %v, want %v", assetRepo.deletedRevID, revisionID)
	}

	// 验证 revision 被完成
	if repo.completeCalls != 1 {
		t.Fatalf("CompleteParse calls = %d, want 1", repo.completeCalls)
	}
	if revision.Status != value.DocumentRevisionReady {
		t.Fatalf("revision status = %v, want ready", revision.Status)
	}

	// 验证 markdown 中的图片引用被替换
	if revision.NormalizedMarkdown == markdown {
		t.Fatal("markdown was not rewritten (image ref should be replaced)")
	}
}

func TestCompleteAsyncParseWithoutImagesSkipsAssetRepo(t *testing.T) {
	ctx := context.Background()
	workspaceID := uuid.New()
	revisionID := uuid.New()

	revision := &model.DocumentRevision{
		ID:          revisionID,
		WorkspaceID: workspaceID,
		Status:      value.DocumentRevisionPending,
		Kind:        value.DocumentKindFile,
	}
	repo := &fakeRevisionRepository{revision: revision}
	assetRepo := &fakeAssetRepositoryForPipeline{}

	p := &DocumentPipeline{
		revisions: repo,
		assets:    assetRepo,
	}

	markdown := "# 标题\n\n纯文本，没有图片"
	parsed := &parserport.ParsedDocument{
		Markdown: markdown,
		Manifest: model.ParseManifest{
			Version:       model.CurrentParseManifestVersion,
			Parser:        "pdf",
			ParserVersion: 1,
			Blocks: []model.ParsedBlock{
				{Sequence: 0, Kind: model.BlockKindParagraph, NormalizedStart: 0, NormalizedEnd: len(markdown)},
			},
		},
	}

	cfg := config.AssetsStorageConfig{
		MaxCountPerDocument: 500,
		MaxImageSizeBytes:   10 * 1024 * 1024,
		AllowedMimeTypes:    []string{"image/png"},
	}
	assetResolver := NewAssetResolver(&fakeAssetStoreForPipeline{}, &http.Client{}, cfg, workspaceID, uuid.New(), revisionID)

	err := p.CompleteAsyncParse(ctx, workspaceID, revisionID, parsed, assetResolver)
	if err != nil {
		t.Fatalf("CompleteAsyncParse() error = %v", err)
	}

	// 没有图片 → 不应该调用 CreateAssets 或 DeleteAssetsByRevision
	if assetRepo.createCalls != 0 {
		t.Fatalf("CreateAssets calls = %d, want 0 (no images)", assetRepo.createCalls)
	}
	if assetRepo.deleteCalls != 0 {
		t.Fatalf("DeleteAssetsByRevision calls = %d, want 0 (no images)", assetRepo.deleteCalls)
	}

	// revision 仍应被完成
	if repo.completeCalls != 1 {
		t.Fatalf("CompleteParse calls = %d, want 1", repo.completeCalls)
	}
}
