package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// fakeTelemetryStore 记录落库调用，可注入失败与 panic。
type fakeTelemetryStore struct {
	mu          sync.Mutex
	citations   []model.SearchCitation
	queryLogs   []model.SearchQueryLogEntry
	calls       int
	insertErr   error
	insertPanic bool
	block       chan struct{}
}

func (f *fakeTelemetryStore) InsertCitations(_ context.Context, entries []model.SearchCitation) error {
	f.mu.Lock()
	f.calls++
	if f.insertPanic {
		f.mu.Unlock()
		panic("boom")
	}
	err := f.insertErr
	f.mu.Unlock()
	if f.block != nil {
		<-f.block
	}
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.citations = append(f.citations, entries...)
	f.mu.Unlock()
	return nil
}

func (f *fakeTelemetryStore) InsertQueryLogEntries(_ context.Context, entries []model.SearchQueryLogEntry) error {
	f.mu.Lock()
	f.calls++
	err := f.insertErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.queryLogs = append(f.queryLogs, entries...)
	f.mu.Unlock()
	return nil
}

func (f *fakeTelemetryStore) snapshot() ([]model.SearchCitation, []model.SearchQueryLogEntry, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.citations, f.queryLogs, f.calls
}

func validCitation(runID uuid.UUID, rank int) model.SearchCitation {
	return model.SearchCitation{
		ID: uuid.New(), WorkspaceID: uuid.New(), KbID: uuid.New(),
		SearchRunID: runID, DocumentID: uuid.New(), ChunkRevisionID: uuid.New(),
		Rank: rank, RetrievalStatus: value.RetrievalStatusAvailable,
	}
}

func TestCitationRecorderFlushesBatches(t *testing.T) {
	store := &fakeTelemetryStore{}
	recorder := NewCitationRecorder(store, CitationRecorderConfig{
		BufferSize: 8, FlushInterval: 10 * time.Millisecond, FlushBatchSize: 100,
	}, nil)

	runID := uuid.New()
	citations := []model.SearchCitation{validCitation(runID, 1), validCitation(runID, 2)}
	logs := []model.SearchQueryLogEntry{{
		ID: uuid.New(), WorkspaceID: citations[0].WorkspaceID, KbID: citations[0].KbID,
		SearchRunID: runID, QueryText: "发票抬头怎么改", QueryHash: "h", QueryChars: 7,
		RetrievalStatus: value.RetrievalStatusEmpty,
	}}
	recorder.RecordCitations(citations, logs)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder.Close(ctx)

	gotCitations, gotLogs, _ := store.snapshot()
	require.Len(t, gotCitations, 2, "Close 应 flush 全部残量")
	require.Len(t, gotLogs, 1)
	require.Equal(t, "发票抬头怎么改", gotLogs[0].QueryText)
	require.Equal(t, int64(0), recorder.Dropped())
}

func TestCitationRecorderDropsWhenBufferFull(t *testing.T) {
	// block 让 flush 停住，缓冲迅速填满。
	block := make(chan struct{})
	store := &fakeTelemetryStore{block: block}
	recorder := NewCitationRecorder(store, CitationRecorderConfig{
		BufferSize: 2, FlushInterval: 5 * time.Millisecond, FlushBatchSize: 100,
	}, nil)

	runID := uuid.New()
	// 第一批进缓冲即被 drain 取走（block 卡在 flush），后续批次填满缓冲后开始丢弃。
	for i := 0; i < 20; i++ {
		recorder.RecordCitations([]model.SearchCitation{validCitation(runID, i+1)}, nil)
	}
	require.Greater(t, recorder.Dropped(), int64(0), "缓冲满后必须有丢弃计数")
	close(block)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder.Close(ctx)
}

func TestCitationRecorderNilStoreIsNoop(t *testing.T) {
	recorder := NewCitationRecorder(nil, CitationRecorderConfig{}, nil)
	require.NotNil(t, recorder)
	recorder.RecordCitations([]model.SearchCitation{validCitation(uuid.New(), 1)}, nil)
	require.Equal(t, int64(0), recorder.Dropped())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder.Close(ctx)
}

func TestCitationRecorderSwallowsInsertError(t *testing.T) {
	store := &fakeTelemetryStore{insertErr: errors.New("db down")}
	recorder := NewCitationRecorder(store, CitationRecorderConfig{
		BufferSize: 4, FlushInterval: 5 * time.Millisecond, FlushBatchSize: 100,
	}, nil)
	recorder.RecordCitations([]model.SearchCitation{validCitation(uuid.New(), 1)}, nil)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, _, calls := store.snapshot(); calls > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, _, calls := store.snapshot()
	require.Greater(t, calls, 0, "flush 必须已尝试落库")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder.Close(ctx)
	// 落库失败不 panic、不阻塞——Close 正常返回即证明。
}

func TestCitationRecorderRecoversFromFlushPanic(t *testing.T) {
	store := &fakeTelemetryStore{insertPanic: true}
	recorder := NewCitationRecorder(store, CitationRecorderConfig{
		BufferSize: 4, FlushInterval: 5 * time.Millisecond, FlushBatchSize: 100,
	}, nil)
	recorder.RecordCitations([]model.SearchCitation{validCitation(uuid.New(), 1)}, nil)

	// flush panic 后 flusher 必须继续存活（Close 能正常完成即为证据）。
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		recorder.Close(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("flusher panic 后 Close 未返回，goroutine 被拖垮")
	}
}

func TestCitationRecorderEmptyRecordIsNoop(t *testing.T) {
	store := &fakeTelemetryStore{}
	recorder := NewCitationRecorder(store, CitationRecorderConfig{}, nil)
	recorder.RecordCitations(nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder.Close(ctx)
	_, _, calls := store.snapshot()
	require.Equal(t, 0, calls, "空批次不应触发落库")
}

func TestCitationRecorderSplitsLargeBatches(t *testing.T) {
	store := &fakeTelemetryStore{}
	recorder := NewCitationRecorder(store, CitationRecorderConfig{
		BufferSize: 64, FlushInterval: 10 * time.Millisecond, FlushBatchSize: 5,
	}, nil)
	runID := uuid.New()
	for i := 0; i < 3; i++ {
		batch := make([]model.SearchCitation, 0, 4)
		for j := 0; j < 4; j++ {
			batch = append(batch, validCitation(runID, j+1))
		}
		recorder.RecordCitations(batch, nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder.Close(ctx)
	got, _, calls := store.snapshot()
	require.Len(t, got, 12, "全部行最终落库")
	require.GreaterOrEqual(t, calls, 3, "12 行 / batch=5 至少分 3 次落库")
}
