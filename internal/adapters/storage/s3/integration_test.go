//go:build integration

package s3

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	portstorage "github.com/dajee/langhuan/internal/ports/storage"
	"github.com/google/uuid"
)

// TestS3StoreRustFSRoundTrip 验证 S3 适配器在真实 S3-compatible 存储
// （RustFS / MinIO）上的 Put/Open/Delete round-trip。
//
// 通过环境变量配置：
//
//	LANGHUAN_TEST_S3_ENDPOINT  (默认 http://127.0.0.1:19000)
//	LANGHUAN_TEST_S3_BUCKET    (默认 langhuan-test)
//	LANGHUAN_TEST_S3_ACCESS_KEY (默认 rustfsadmin)
//	LANGHUAN_TEST_S3_SECRET_KEY (默认 rustfsadmin)
//	LANGHUAN_TEST_S3_REGION    (默认 us-east-1)
//
// 未提供 endpoint 时跳过，不依赖任何外部服务。
func TestS3StoreRustFSRoundTrip(t *testing.T) {
	endpoint := os.Getenv("LANGHUAN_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("LANGHUAN_TEST_S3_ENDPOINT 未设置，跳过 S3 集成测试")
	}
	bucket := os.Getenv("LANGHUAN_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "langhuan-test"
	}
	accessKey := os.Getenv("LANGHUAN_TEST_S3_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "rustfsadmin"
	}
	secretKey := os.Getenv("LANGHUAN_TEST_S3_SECRET_KEY")
	if secretKey == "" {
		secretKey = "rustfsadmin"
	}
	region := os.Getenv("LANGHUAN_TEST_S3_REGION")
	if region == "" {
		region = "us-east-1"
	}

	ctx := context.Background()
	store, err := NewStore(ctx, Config{
		Endpoint:       endpoint,
		Region:         region,
		Bucket:         bucket,
		AccessKey:      accessKey,
		SecretKey:      secretKey,
		ForcePathStyle: true,
		PublicBaseURL:  endpoint + "/" + bucket,
	})
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	rawStore := store.NewRawDocumentStore()
	assetStore := store.NewAssetStore()

	// raw document round-trip
	ws := uuid.New()
	kb := uuid.New()
	doc := uuid.New()
	content := "integration test pdf content"
	obj, err := rawStore.Put(ctx, portstorage.RawDocumentInput{
		WorkspaceID:     ws,
		KnowledgeBaseID: kb,
		DocumentID:      doc,
		FileName:        "test.pdf",
		ContentType:     "application/pdf",
		Reader:          strings.NewReader(content),
		SizeBytes:       int64(len(content)),
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	reader, err := rawStore.Open(ctx, obj.Key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, []byte(content)) {
		t.Fatalf("round-trip content mismatch: got %q", string(got))
	}

	if err := rawStore.Delete(ctx, obj.Key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// asset round-trip
	assetKey := "assets/" + ws.String() + "/" + doc.String() + "/rev/" + uuid.New().String() + ".png"
	assetData := []byte("fake png")
	assetObj, err := assetStore.Put(ctx, portstorage.ObjectInput{
		Key:      assetKey,
		MimeType: "image/png",
		Data:     assetData,
	})
	if err != nil {
		t.Fatalf("AssetStore.Put() error = %v", err)
	}
	if assetObj.PublicURL == "" {
		t.Fatal("asset PublicURL should not be empty")
	}
	if err := assetStore.Delete(ctx, assetKey); err != nil {
		t.Fatalf("AssetStore.Delete() error = %v", err)
	}
}
