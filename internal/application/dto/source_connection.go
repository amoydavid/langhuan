package dto

import (
	"time"

	"github.com/google/uuid"
)

// SourceConnection 是来源连接的无凭证 API 表示。
// AppSecret 永远不会出现在响应中；EndpointURL 是非敏感 config
// （firecrawl endpoint 覆盖 / browser CDP 端点），供管理台展示。
type SourceConnection struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Provider    string    `json:"provider"`
	Name        string    `json:"name"`
	AppID       string    `json:"app_id"`
	EndpointURL string    `json:"endpoint_url"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SourceConnectionFromModel 把领域模型转成无凭证 DTO。
func SourceConnectionFromModel(conn *SourceConnectionInput) SourceConnection {
	return SourceConnection{
		ID: conn.ID, WorkspaceID: conn.WorkspaceID, Provider: conn.Provider,
		Name: conn.Name, AppID: conn.AppID, EndpointURL: conn.EndpointURL, Status: conn.Status,
		CreatedAt: conn.CreatedAt, UpdatedAt: conn.UpdatedAt,
	}
}

// SourceConnectionInput 是转换 DTO 时需要的字段（从 model.SourceConnection 提取）。
type SourceConnectionInput struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Provider    string
	Name        string
	AppID       string
	EndpointURL string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SourceConnectionTestResult 是来源连接「连接测试」的非持久化结果（spec §9.4）。
// 成功与失败都以 OK + Message 表达（成功含耗时、失败含原因，均不含凭证），
// 结果不落库。
type SourceConnectionTestResult struct {
	OK         bool   `json:"ok"`
	Provider   string `json:"provider"`
	DurationMS int64  `json:"duration_ms"`
	Message    string `json:"message"`
}
