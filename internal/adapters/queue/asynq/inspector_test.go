package asynq

import (
	"context"
	"errors"
	"testing"
	"time"

	hibikenasynq "github.com/hibiken/asynq"
)

type fakeInspector struct {
	queues        []string
	queueInfo     map[string]*hibikenasynq.QueueInfo
	archived      map[string][]*hibikenasynq.TaskInfo
	runTaskErr    error
	deleteTaskErr error
	runTaskCalls  []struct{ queue, id string }
	deleteCalls   []struct{ queue, id string }
	closed        bool
}

func (f *fakeInspector) Queues() ([]string, error) { return f.queues, nil }
func (f *fakeInspector) GetQueueInfo(queue string) (*hibikenasynq.QueueInfo, error) {
	info, ok := f.queueInfo[queue]
	if !ok {
		return nil, errors.New("queue not found")
	}
	return info, nil
}
func (f *fakeInspector) ListArchivedTasks(queue string, _ ...hibikenasynq.ListOption) ([]*hibikenasynq.TaskInfo, error) {
	return f.archived[queue], nil
}
func (f *fakeInspector) RunTask(queue, id string) error {
	f.runTaskCalls = append(f.runTaskCalls, struct{ queue, id string }{queue, id})
	return f.runTaskErr
}
func (f *fakeInspector) DeleteTask(queue, id string) error {
	f.deleteCalls = append(f.deleteCalls, struct{ queue, id string }{queue, id})
	return f.deleteTaskErr
}
func (f *fakeInspector) Close() error { f.closed = true; return nil }

func TestInspectorSnapshots(t *testing.T) {
	fake := &fakeInspector{
		queues: []string{"default", "critical"},
		queueInfo: map[string]*hibikenasynq.QueueInfo{
			"default":  {Queue: "default", Size: 10, Pending: 5, Active: 2, Retry: 1, Archived: 2, ProcessedTotal: 100, FailedTotal: 3},
			"critical": {Queue: "critical", Size: 4, Pending: 4, Active: 0, ProcessedTotal: 50},
		},
	}
	insp := NewQueueInspector(fake)

	snapshots, err := insp.Snapshots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshots len = %d, want 2", len(snapshots))
	}
	// 找到 default 队列校验字段。
	var def *QueueSnapshot
	for i := range snapshots {
		if snapshots[i].Queue == "default" {
			def = &snapshots[i]
		}
	}
	if def == nil {
		t.Fatal("default queue not found in snapshots")
	}
	if def.Pending != 5 || def.Archived != 2 || def.ProcessedTotal != 100 {
		t.Fatalf("default snapshot = %#v", def)
	}

	// TotalPending = 5 + 4 = 9。
	total, err := insp.TotalPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if total != 9 {
		t.Fatalf("TotalPending = %d, want 9", total)
	}
}

func TestInspectorListDead(t *testing.T) {
	fake := &fakeInspector{
		queues: []string{"default"},
		queueInfo: map[string]*hibikenasynq.QueueInfo{
			"default": {Queue: "default"},
		},
		archived: map[string][]*hibikenasynq.TaskInfo{
			"default": {
				{ID: "task-1", Type: "document_index", LastErr: "boom", Retried: 4, MaxRetry: 4, LastFailedAt: time.UnixMilli(1000)},
				{ID: "task-2", Type: "document_parse_start", LastErr: "timeout", Retried: 2, MaxRetry: 4},
			},
		},
	}
	insp := NewQueueInspector(fake)

	dead, err := insp.ListDead(context.Background(), "default", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 2 {
		t.Fatalf("dead len = %d, want 2", len(dead))
	}
	if dead[0].ID != "task-1" || dead[0].Type != "document_index" {
		t.Fatalf("dead[0] = %#v", dead[0])
	}
	if dead[0].Retried != 4 || dead[0].MaxRetry != 4 {
		t.Fatalf("dead[0] retry counts = %d/%d", dead[0].Retried, dead[0].MaxRetry)
	}
	// DeadTask 不应携带 Payload 字段（结构体本身就没有，确保转换不泄漏）。
	for _, d := range dead {
		// 结构体没有 Payload 字段即可；这里校验关键字段存在。
		if d.ID == "" || d.Type == "" {
			t.Fatalf("dead task missing id/type: %#v", d)
		}
	}
}

func TestInspectorRetryDead(t *testing.T) {
	fake := &fakeInspector{
		runTaskErr: nil,
	}
	insp := NewQueueInspector(fake)

	if err := insp.RetryDead(context.Background(), "default", "task-1"); err != nil {
		t.Fatal(err)
	}
	if len(fake.runTaskCalls) != 1 || fake.runTaskCalls[0].queue != "default" || fake.runTaskCalls[0].id != "task-1" {
		t.Fatalf("RunTask calls = %#v", fake.runTaskCalls)
	}
}

func TestInspectorRetryDeadError(t *testing.T) {
	fake := &fakeInspector{
		runTaskErr: errors.New("task not found"),
	}
	insp := NewQueueInspector(fake)

	err := insp.RetryDead(context.Background(), "default", "missing")
	if err == nil {
		t.Fatal("error = nil, want error")
	}
}

func TestInspectorDeleteDead(t *testing.T) {
	fake := &fakeInspector{}
	insp := NewQueueInspector(fake)

	if err := insp.DeleteDead(context.Background(), "default", "task-1"); err != nil {
		t.Fatal(err)
	}
	if len(fake.deleteCalls) != 1 || fake.deleteCalls[0].id != "task-1" {
		t.Fatalf("DeleteTask calls = %#v", fake.deleteCalls)
	}
}

func TestInspectorClose(t *testing.T) {
	fake := &fakeInspector{}
	insp := NewQueueInspector(fake)
	if err := insp.Close(); err != nil {
		t.Fatal(err)
	}
	if !fake.closed {
		t.Fatal("inspector not closed")
	}
}
