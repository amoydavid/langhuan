package service

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/storage"
)

// --- cleanup fakes -----------------------------------------------------

// fakeCleanupStore 是 SourceCleanupStore 的内存实现，记录状态推进。
type fakeCleanupStore struct {
	job       *model.Job
	objects   []CleanupObject
	getErr    error
	succErr   error
	failErr   error
	pending   []DueCleanupJob
	listErr   error
	succeeded bool
	failed    bool
	failMsg   string
}

func (s *fakeCleanupStore) GetSourceCleanupJob(_ context.Context, _, _ uuid.UUID) (*model.Job, []CleanupObject, error) {
	if s.getErr != nil {
		return nil, nil, s.getErr
	}
	return s.job, s.objects, nil
}

func (s *fakeCleanupStore) MarkSourceCleanupJobSucceeded(_ context.Context, _, _ uuid.UUID) error {
	if s.succErr != nil {
		return s.succErr
	}
	s.succeeded = true
	return nil
}

func (s *fakeCleanupStore) MarkSourceCleanupJobFailed(_ context.Context, _, _ uuid.UUID, message string) error {
	if s.failErr != nil {
		return s.failErr
	}
	s.failed = true
	s.failMsg = message
	return nil
}

func (s *fakeCleanupStore) ListPendingSourceCleanupJobs(_ context.Context) ([]DueCleanupJob, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.pending, nil
}

// fakeCleanupRawStore 记录 raw/parser 对象的删除调用，可注入错误。
// 实现 storage.RawDocumentStore（Put/Open 在清理路径不使用）。
type fakeCleanupRawStore struct {
	deletes   []string
	deleteErr error
}

func (s *fakeCleanupRawStore) Put(context.Context, storage.RawDocumentInput) (*storage.RawDocumentObject, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeCleanupRawStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeCleanupRawStore) Delete(_ context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	return s.deleteErr
}

// fakeCleanupAssetStore 记录 asset 对象的删除调用，可注入错误。
// 实现 storage.AssetStore（Put/Open 在清理路径不使用）。
type fakeCleanupAssetStore struct {
	deletes   []string
	deleteErr error
}

func (s *fakeCleanupAssetStore) Put(context.Context, storage.ObjectInput) (*storage.StoredObject, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeCleanupAssetStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeCleanupAssetStore) Delete(_ context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	return s.deleteErr
}

// newCleanupHarness 构造一个 cleanup service + fakes，便于断言。
func newCleanupHarness(objects []CleanupObject) (*SourceCleanupService, *fakeCleanupStore, *fakeCleanupRawStore, *fakeCleanupAssetStore) {
	store := &fakeCleanupStore{
		job:     &model.Job{ID: uuid.New(), Status: value.JobStatusPending},
		objects: objects,
	}
	raw := &fakeCleanupRawStore{}
	asset := &fakeCleanupAssetStore{}
	svc := NewSourceCleanupService(SourceCleanupServiceDeps{
		Store: store, RawStore: raw, AssetStore: asset,
		Logger: &recordingLogger{},
	})
	return svc, store, raw, asset
}

// --- tests -------------------------------------------------------------

// TestCleanupTreatsMissingObjectAsSuccess 验证对象已不存在（ErrObjectNotFound）视为成功并标记 Job 成功。
func TestCleanupTreatsMissingObjectAsSuccess(t *testing.T) {
	objects := []CleanupObject{{Key: "raw/missing.md", Store: "raw"}}
	svc, store, raw, _ := newCleanupHarness(objects)
	raw.deleteErr = storage.ErrObjectNotFound

	if err := svc.Run(context.Background(), uuid.New(), uuid.New(), store.job.ID); err != nil {
		t.Fatalf("Run err = %v, want nil (missing object is success)", err)
	}
	if !store.succeeded {
		t.Fatal("Job 应被标记 succeeded")
	}
	if store.failed {
		t.Fatal("Job 不应被标记 failed")
	}
	if len(raw.deletes) != 1 || raw.deletes[0] != "raw/missing.md" {
		t.Fatalf("raw deletes = %v, want [raw/missing.md]", raw.deletes)
	}
}

// TestCleanupAllKeysDeletedMarksSucceeded 验证全部 key 删除成功后标记 Job 成功。
func TestCleanupAllKeysDeletedMarksSucceeded(t *testing.T) {
	objects := []CleanupObject{
		{Key: "raw/a.md", Store: "raw"},
		{Key: "parser/b.md", Store: "parser"},
		{Key: "assets/c.png", Store: "asset"},
	}
	svc, store, raw, asset := newCleanupHarness(objects)

	if err := svc.Run(context.Background(), uuid.New(), uuid.New(), store.job.ID); err != nil {
		t.Fatalf("Run err = %v, want nil", err)
	}
	if !store.succeeded {
		t.Fatal("Job 应被标记 succeeded")
	}
	// raw/parser 都路由到 RawStore。
	if len(raw.deletes) != 2 {
		t.Fatalf("raw deletes = %v, want 2 (raw+parser)", raw.deletes)
	}
	if len(asset.deletes) != 1 || asset.deletes[0] != "assets/c.png" {
		t.Fatalf("asset deletes = %v, want [assets/c.png]", asset.deletes)
	}
}

// TestCleanupRealDeleteErrorMarksFailed 验证非 not-found 的删除错误标记 Job failed 且 Run 返回错误（可重试）。
func TestCleanupRealDeleteErrorMarksFailed(t *testing.T) {
	objects := []CleanupObject{{Key: "raw/bad.md", Store: "raw"}}
	svc, store, raw, _ := newCleanupHarness(objects)
	raw.deleteErr = errors.New("s3 timeout")

	err := svc.Run(context.Background(), uuid.New(), uuid.New(), store.job.ID)
	if err == nil {
		t.Fatal("Run err = nil, want error on real delete failure")
	}
	if store.succeeded {
		t.Fatal("Job 不应被标记 succeeded")
	}
	if !store.failed {
		t.Fatal("Job 应被标记 failed")
	}
	if store.failMsg == "" {
		t.Fatal("failed message 不应为空")
	}
}

// TestCleanupPartialFailureMarksFailed 验证多个 key 中部分失败时标记 Job failed。
func TestCleanupPartialFailureMarksFailed(t *testing.T) {
	objects := []CleanupObject{
		{Key: "raw/ok.md", Store: "raw"},
		{Key: "assets/bad.png", Store: "asset"},
	}
	svc, store, _, asset := newCleanupHarness(objects)
	asset.deleteErr = errors.New("asset store down")

	err := svc.Run(context.Background(), uuid.New(), uuid.New(), store.job.ID)
	if err == nil {
		t.Fatal("Run err = nil, want error on partial failure")
	}
	if !store.failed {
		t.Fatal("Job 应被标记 failed（部分失败）")
	}
}

// TestCleanupIdempotentReRunOnCompletedJob 验证对已完成 Job 的重复执行直接返回 nil（幂等）。
func TestCleanupIdempotentReRunOnCompletedJob(t *testing.T) {
	objects := []CleanupObject{{Key: "raw/x.md", Store: "raw"}}
	svc, store, raw, _ := newCleanupHarness(objects)
	store.job.Status = value.JobStatusSucceeded

	if err := svc.Run(context.Background(), uuid.New(), uuid.New(), store.job.ID); err != nil {
		t.Fatalf("Run err = %v, want nil (already completed)", err)
	}
	if len(raw.deletes) != 0 {
		t.Fatalf("已完成 Job 不应再删除对象，raw deletes = %v", raw.deletes)
	}
}

// TestCleanupRejectsMissingLineage 验证 lineage 缺失返回校验错误。
func TestCleanupRejectsMissingLineage(t *testing.T) {
	svc, _, _, _ := newCleanupHarness(nil)
	if err := svc.Run(context.Background(), uuid.Nil, uuid.New(), uuid.New()); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// TestCleanupRejectsUnknownStoreType 验证未知 store 类型标记 Job failed。
func TestCleanupRejectsUnknownStoreType(t *testing.T) {
	objects := []CleanupObject{{Key: "weird/x", Store: "unknown"}}
	svc, store, _, _ := newCleanupHarness(objects)

	if err := svc.Run(context.Background(), uuid.New(), uuid.New(), store.job.ID); err == nil {
		t.Fatal("Run err = nil, want error on unknown store type")
	}
	if !store.failed {
		t.Fatal("Job 应被标记 failed（未知 store 类型）")
	}
}

// TestSourceCleanupObjectBatchSizeConstant 验证批次大小常量符合预期。
func TestSourceCleanupObjectBatchSizeConstant(t *testing.T) {
	if SourceCleanupObjectBatchSize != 100 {
		t.Fatalf("SourceCleanupObjectBatchSize = %d, want 100", SourceCleanupObjectBatchSize)
	}
}
