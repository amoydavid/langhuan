package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	portstorage "github.com/dajee/langhuan/internal/ports/storage"
	"github.com/google/uuid"
)

// fakeS3 是 s3API 的内存实现，记录操作并返回可断言的结果。
type fakeS3 struct {
	objects    map[string][]byte // key -> content
	putErr     error
	getErr     error
	deleteErr  error
	lastBucket string
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: make(map[string][]byte)}
}

func (f *fakeS3) PutObject(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.lastBucket = *input.Bucket
	if f.putErr != nil {
		return nil, f.putErr
	}
	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	f.objects[*input.Key] = data
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) GetObject(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	data, ok := f.objects[*input.Key]
	if !ok {
		return nil, errors.New("not found")
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func (f *fakeS3) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	delete(f.objects, *input.Key)
	return &s3.DeleteObjectOutput{}, nil
}

func TestS3RawDocumentStorePutComputesHash(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	store := newStoreWithClient(fake, "test-bucket", "http://cdn.example.com/bucket")
	rawStore := store.NewRawDocumentStore()

	ws := uuid.New()
	kb := uuid.New()
	doc := uuid.New()
	content := "fake pdf content"
	obj, err := rawStore.Put(ctx, portstorage.RawDocumentInput{
		WorkspaceID:     ws,
		KnowledgeBaseID: kb,
		DocumentID:      doc,
		FileName:        "report.pdf",
		ContentType:     "application/pdf",
		Reader:          strings.NewReader(content),
		SizeBytes:       int64(len(content)),
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if obj.ContentType != "application/pdf" {
		t.Fatalf("ContentType = %q", obj.ContentType)
	}
	if obj.SizeBytes != int64(len(content)) {
		t.Fatalf("SizeBytes = %d", obj.SizeBytes)
	}
	if !strings.HasPrefix(obj.Key, "raw-documents/") {
		t.Fatalf("Key = %q", obj.Key)
	}
	if !strings.HasSuffix(obj.Key, "/original.pdf") {
		t.Fatalf("Key = %q, want .pdf suffix", obj.Key)
	}
	if fake.lastBucket != "test-bucket" {
		t.Fatalf("bucket = %q", fake.lastBucket)
	}
	// 验证内容确实写进去了
	stored, ok := fake.objects[obj.Key]
	if !ok {
		t.Fatal("object not stored in fake S3")
	}
	if string(stored) != content {
		t.Fatalf("stored content = %q", string(stored))
	}
}

func TestS3RawDocumentStoreOpenReturnsReader(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	store := newStoreWithClient(fake, "test-bucket", "")
	rawStore := store.NewRawDocumentStore()

	key := "raw-documents/ws/kb/doc/rev/original.pdf"
	fake.objects[key] = []byte("pdf bytes")

	reader, err := rawStore.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "pdf bytes" {
		t.Fatalf("content = %q", string(data))
	}
}

func TestS3RawDocumentStoreDelete(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	store := newStoreWithClient(fake, "test-bucket", "")
	rawStore := store.NewRawDocumentStore()

	key := "raw-documents/ws/kb/doc/rev/original.pdf"
	fake.objects[key] = []byte("data")

	if err := rawStore.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok := fake.objects[key]; ok {
		t.Fatal("object still exists after delete")
	}
}

func TestS3AssetStorePut(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	store := newStoreWithClient(fake, "test-bucket", "http://cdn.example.com/bucket")
	assetStore := store.NewAssetStore()

	key := "assets/ws/doc/rev/asset.png"
	data := []byte("png bytes")
	obj, err := assetStore.Put(ctx, portstorage.ObjectInput{
		Key:      key,
		MimeType: "image/png",
		Data:     data,
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if obj.Key != key {
		t.Fatalf("Key = %q", obj.Key)
	}
	if obj.SizeBytes != int64(len(data)) {
		t.Fatalf("SizeBytes = %d", obj.SizeBytes)
	}
	if obj.PublicURL != "http://cdn.example.com/bucket/assets/ws/doc/rev/asset.png" {
		t.Fatalf("PublicURL = %q", obj.PublicURL)
	}
}

func TestS3AssetStoreDelete(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	store := newStoreWithClient(fake, "test-bucket", "")
	assetStore := store.NewAssetStore()

	key := "assets/ws/doc/rev/asset.png"
	fake.objects[key] = []byte("data")

	if err := assetStore.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok := fake.objects[key]; ok {
		t.Fatal("object still exists after delete")
	}
}

func TestS3StorePublicURLComposition(t *testing.T) {
	store := newStoreWithClient(newFakeS3(), "b", "http://127.0.0.1:19000/langhuan-test")
	got := store.composePublicURL("assets/ws/doc/rev/asset.png")
	want := "http://127.0.0.1:19000/langhuan-test/assets/ws/doc/rev/asset.png"
	if got != want {
		t.Fatalf("composePublicURL = %q, want %q", got, want)
	}

	// 空 base URL 返回空
	store2 := newStoreWithClient(newFakeS3(), "b", "")
	if got := store2.composePublicURL("assets/x.png"); got != "" {
		t.Fatalf("empty base url should return empty, got %q", got)
	}
}

func TestS3StorePutObjectForRawMarkdown(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	store := newStoreWithClient(fake, "test-bucket", "http://cdn/bucket")

	key := "parser-results/ws/doc/rev/job/raw.md"
	data := []byte("# heading\n")
	obj, err := store.PutObject(ctx, key, "text/markdown", data)
	if err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}
	if obj.Key != key {
		t.Fatalf("Key = %q", obj.Key)
	}
	if obj.PublicURL != "http://cdn/bucket/parser-results/ws/doc/rev/job/raw.md" {
		t.Fatalf("PublicURL = %q", obj.PublicURL)
	}
	stored, ok := fake.objects[key]
	if !ok {
		t.Fatal("raw markdown not stored")
	}
	if string(stored) != string(data) {
		t.Fatalf("stored = %q", string(stored))
	}
}

func TestS3StorePutError(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	fake.putErr = errors.New("S3 unavailable")
	store := newStoreWithClient(fake, "b", "")
	rawStore := store.NewRawDocumentStore()

	_, err := rawStore.Put(ctx, portstorage.RawDocumentInput{
		WorkspaceID:     uuid.New(),
		KnowledgeBaseID: uuid.New(),
		DocumentID:      uuid.New(),
		FileName:        "x.pdf",
		Reader:          strings.NewReader("x"),
	})
	if err == nil {
		t.Fatal("expected error from S3 Put failure")
	}
}
