package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// SelectedSourceConnection 是 Selector 解密后的结果，供 SourceConnector 使用。
type SelectedSourceConnection struct {
	ConnectionID uuid.UUID
	WorkspaceID  uuid.UUID
	AppID        string
	AppSecret    []byte
}

// SourceConnectionSelector 解密来源连接凭证，供同步运行时取用。
type SourceConnectionSelector struct {
	repo   SourceConnectionRepository
	cipher SourceConnectionCipher
}

func NewSourceConnectionSelector(repo SourceConnectionRepository, cipher SourceConnectionCipher) *SourceConnectionSelector {
	return &SourceConnectionSelector{repo: repo, cipher: cipher}
}

// Select 按 workspace + connectionID 解密凭证，返回 app_id + 明文 app_secret。
func (s *SourceConnectionSelector) Select(ctx context.Context, workspaceID, connectionID uuid.UUID) (SelectedSourceConnection, error) {
	conn, err := s.repo.Get(ctx, workspaceID, connectionID)
	if err != nil {
		return SelectedSourceConnection{}, err
	}
	if conn.Status != "active" {
		return SelectedSourceConnection{}, fmt.Errorf("来源连接 %s 已停用", connectionID)
	}
	plaintext, err := s.cipher.Decrypt(conn.ID, conn.CredentialsCiphertext)
	if err != nil {
		return SelectedSourceConnection{}, fmt.Errorf("解密来源连接凭证失败: %w", err)
	}
	appID, _ := conn.Config["app_id"].(string)
	return SelectedSourceConnection{
		ConnectionID: conn.ID,
		WorkspaceID:  conn.WorkspaceID,
		AppID:        appID,
		AppSecret:    plaintext,
	}, nil
}
