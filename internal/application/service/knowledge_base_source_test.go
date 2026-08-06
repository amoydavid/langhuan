package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// fakeKBSyncEnqueuer 记录 EnqueueSync 调用，可注入错误。
type fakeKBSyncEnqueuer struct {
	calls []kbEnqueueCall
	jobs  map[uuid.UUID]*model.Job
	err   error
}

type kbEnqueueCall struct {
	WorkspaceID uuid.UUID
	KBID        uuid.UUID
}

func (e *fakeKBSyncEnqueuer) EnqueueSync(_ context.Context, workspaceID, kbID uuid.UUID) (*model.Job, error) {
	e.calls = append(e.calls, kbEnqueueCall{WorkspaceID: workspaceID, KBID: kbID})
	if e.err != nil {
		return nil, e.err
	}
	if e.jobs == nil {
		e.jobs = map[uuid.UUID]*model.Job{}
	}
	job := &model.Job{ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: kbID}
	e.jobs[kbID] = job
	return job, nil
}

// TestCreateFeishuKnowledgeBaseEnqueuesFirstSync 验证创建飞书知识库后触发首次同步。
func TestCreateFeishuKnowledgeBaseEnqueuesFirstSync(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	enqueuer := &fakeKBSyncEnqueuer{}
	service := NewKnowledgeBaseService(repository, repository).WithSyncEnqueuer(enqueuer, nil)

	connID := uuid.New()
	created, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "飞书库", EmbeddingModelID: resolved.Model.ID,
		SourceType:         value.SourceTypeFeishuWiki,
		SourceConfig:       map[string]any{"root_token": "root-node-token", "root_kind": "wiki_node"},
		SourceConnectionID: &connID,
	})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	if created.SourceType != value.SourceTypeFeishuWiki {
		t.Fatalf("created source type = %q, want feishu_wiki", created.SourceType)
	}
	if len(enqueuer.calls) != 1 {
		t.Fatalf("enqueue calls = %d, want 1", len(enqueuer.calls))
	}
	if enqueuer.calls[0].WorkspaceID != workspaceID || enqueuer.calls[0].KBID != created.ID {
		t.Fatalf("enqueue call = %+v", enqueuer.calls[0])
	}
	// 落库的 KB 应携带来源信息。
	persisted := repository.items[created.ID]
	if persisted == nil || persisted.SourceType != value.SourceTypeFeishuWiki {
		t.Fatalf("persisted KB source = %v", persisted)
	}
	if persisted.SourceConnectionID == nil || *persisted.SourceConnectionID != connID {
		t.Fatalf("persisted connection id = %v, want %s", persisted.SourceConnectionID, connID)
	}
}

// TestCreateNonFeishuKnowledgeBaseDoesNotEnqueueSync 验证上传型 KB 创建后不触发同步。
func TestCreateNonFeishuKnowledgeBaseDoesNotEnqueueSync(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	enqueuer := &fakeKBSyncEnqueuer{}
	service := NewKnowledgeBaseService(repository, repository).WithSyncEnqueuer(enqueuer, nil)

	created, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "上传库", EmbeddingModelID: resolved.Model.ID,
	})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	if created.SourceType != value.SourceTypeUpload {
		t.Fatalf("created source type = %q, want upload", created.SourceType)
	}
	if len(enqueuer.calls) != 0 {
		t.Fatalf("enqueue calls = %d, want 0 for non-feishu KB", len(enqueuer.calls))
	}
}

// TestCreateFeishuKnowledgeBaseWithoutEnqueuerSkipsSync 验证未注入 enqueuer 时跳过首次同步（KB 仍创建）。
func TestCreateFeishuKnowledgeBaseWithoutEnqueuerSkipsSync(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	service := NewKnowledgeBaseService(repository, repository) // 无 enqueuer

	connID := uuid.New()
	created, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "飞书库", EmbeddingModelID: resolved.Model.ID,
		SourceType:         value.SourceTypeFeishuDrive,
		SourceConfig:       map[string]any{"root_token": "tok"},
		SourceConnectionID: &connID,
	})
	if err != nil {
		t.Fatalf("Create err = %v", err)
	}
	if created == nil {
		t.Fatal("KB should still be created when enqueuer is nil")
	}
}

// TestCreateFeishuKnowledgeBaseEnqueueFailureDoesNotRollback 验证入队失败不回滚 KB 创建。
func TestCreateFeishuKnowledgeBaseEnqueueFailureDoesNotRollback(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	enqueuer := &fakeKBSyncEnqueuer{err: errors.New("queue unavailable")}
	service := NewKnowledgeBaseService(repository, repository).WithSyncEnqueuer(enqueuer, nil)

	connID := uuid.New()
	created, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "飞书库", EmbeddingModelID: resolved.Model.ID,
		SourceType:         value.SourceTypeFeishuWiki,
		SourceConfig:       map[string]any{"root_token": "tok"},
		SourceConnectionID: &connID,
	})
	if err != nil {
		t.Fatalf("Create err = %v, want nil (enqueue failure should not fail create)", err)
	}
	if created == nil {
		t.Fatal("created KB should not be nil despite enqueue failure")
	}
	if repository.items[created.ID] == nil {
		t.Fatal("KB should be persisted despite enqueue failure")
	}
	if len(enqueuer.calls) != 1 {
		t.Fatalf("enqueue attempts = %d, want 1", len(enqueuer.calls))
	}
}

// TestCreateFeishuKnowledgeBaseRequiresConnectionID 验证飞书来源缺失 connection_id 时返回校验错误。
func TestCreateFeishuKnowledgeBaseRequiresConnectionID(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	enqueuer := &fakeKBSyncEnqueuer{}
	service := NewKnowledgeBaseService(repository, repository).WithSyncEnqueuer(enqueuer, nil)

	_, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "飞书库", EmbeddingModelID: resolved.Model.ID,
		SourceType:   value.SourceTypeFeishuWiki,
		SourceConfig: map[string]any{"root_token": "tok"},
		// SourceConnectionID 缺失
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if len(enqueuer.calls) != 0 {
		t.Fatalf("enqueue calls = %d, want 0 on validation failure", len(enqueuer.calls))
	}
}
