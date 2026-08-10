package service

import (
	"context"
	"fmt"
	"time"
)

// QueueSnapshot 是某一队列的计数快照。
type QueueSnapshot struct {
	Queue          string `json:"queue"`
	Size           int    `json:"size"`
	Pending        int    `json:"pending"`
	Active         int    `json:"active"`
	Scheduled      int    `json:"scheduled"`
	Retry          int    `json:"retry"`
	Dead           int    `json:"dead"`
	ProcessedTotal int    `json:"processed_total"`
	FailedTotal    int    `json:"failed_total"`
}

// DeadTask 是死信任务的脱敏视图（不含 payload 正文）。
type DeadTask struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	LastFailedAt time.Time `json:"last_failed_at"`
	LastError    string    `json:"last_error"`
	Retried      int       `json:"retried"`
	MaxRetry     int       `json:"max_retry"`
}

// QueueInspectorPort 是 asynq Inspector 的抽象接口，便于测试 mock。
// 实现方（asynq.QueueInspector）负责把 asynq 原生类型转换为这里的 service 类型，
// 保持 application 层不依赖 adapter 包。
type QueueInspectorPort interface {
	Snapshots(ctx context.Context) ([]QueueSnapshot, error)
	ListDead(ctx context.Context, queue string, page, pageSize int) ([]DeadTask, error)
	RetryDead(ctx context.Context, queue, taskID string) error
	DeleteDead(ctx context.Context, queue, taskID string) error
}

// QueueAdminService 封装 asynq Inspector 的队列可见性与死信管理能力，
// 供 platform admin 的队列监控端点使用。不在 MCP 暴露（运维操作，非程序化消费）。
type QueueAdminService struct {
	inspector QueueInspectorPort
}

// QueueAdminDeps 装配队列管理服务。
type QueueAdminDeps struct {
	Inspector QueueInspectorPort
}

func NewQueueAdminService(deps QueueAdminDeps) *QueueAdminService {
	return &QueueAdminService{inspector: deps.Inspector}
}

// ListQueues 返回所有队列的计数快照。
func (s *QueueAdminService) ListQueues(ctx context.Context) ([]QueueSnapshot, error) {
	if s.inspector == nil {
		return nil, fmt.Errorf("队列监控未启用")
	}
	return s.inspector.Snapshots(ctx)
}

// ListDead 列出某队列的死信任务。
func (s *QueueAdminService) ListDead(ctx context.Context, queue string, page, pageSize int) ([]DeadTask, error) {
	if s.inspector == nil {
		return nil, fmt.Errorf("队列监控未启用")
	}
	return s.inspector.ListDead(ctx, queue, page, pageSize)
}

// RetryDead 手动重试一个死信任务。
func (s *QueueAdminService) RetryDead(ctx context.Context, queue, taskID string) error {
	if s.inspector == nil {
		return fmt.Errorf("队列监控未启用")
	}
	if taskID == "" {
		return fmt.Errorf("task_id 不能为空")
	}
	return s.inspector.RetryDead(ctx, queue, taskID)
}

// DeleteDead 永久删除一个死信任务。
func (s *QueueAdminService) DeleteDead(ctx context.Context, queue, taskID string) error {
	if s.inspector == nil {
		return fmt.Errorf("队列监控未启用")
	}
	if taskID == "" {
		return fmt.Errorf("task_id 不能为空")
	}
	return s.inspector.DeleteDead(ctx, queue, taskID)
}
