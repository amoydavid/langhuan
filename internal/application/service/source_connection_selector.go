package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrSourceConnectionDisabled 是「连接存在但已停用」的哨兵错误：selector 拒绝
// 为停用连接解密凭证（同步编排与连接测试共用此判定路径）。调用方用 errors.Is
// 区分「用户可自助解决的状态」与其它失败（T12 评审移交 a）。
var ErrSourceConnectionDisabled = errors.New("来源连接已停用")

// SelectedSourceConnection 是 Selector 解密后的结果，供 SourceConnector 使用。
type SelectedSourceConnection struct {
	ConnectionID uuid.UUID
	WorkspaceID  uuid.UUID
	// Provider 是连接的 provider 标识（feishu/firecrawl/jina/browser）。
	// 飞书路径不感知；web 源编排据此选择凭证注入键（api_key/token，spec §3.1）。
	Provider  string
	AppID     string
	AppSecret []byte
	// Config 是连接的非敏感 config（browser 的 CDP endpoint_url、firecrawl 的
	// endpoint 覆盖，spec §3.1）。web 源编排取 endpoint_url 注入 __credentials
	// 子键；密文字段（CredentialsCiphertext）不经此处透出。
	Config map[string]any
}

// SourceConnectionSelector 解密来源连接凭证，供同步运行时取用。
type SourceConnectionSelector struct {
	repo   SourceConnectionRepository
	cipher SourceConnectionCipher
}

func NewSourceConnectionSelector(repo SourceConnectionRepository, cipher SourceConnectionCipher) *SourceConnectionSelector {
	return &SourceConnectionSelector{repo: repo, cipher: cipher}
}

// Select 按 workspace + connectionID 解密凭证，返回 provider + app_id + 明文凭证。
// 空密文（browser 无 token 连接，T12 起合法）不调用 cipher、直接返回空凭证：
// AES-GCM 对空输入必然报「密文格式无效」，会让同步编排与连接测试全部卡死。
func (s *SourceConnectionSelector) Select(ctx context.Context, workspaceID, connectionID uuid.UUID) (SelectedSourceConnection, error) {
	conn, err := s.repo.Get(ctx, workspaceID, connectionID)
	if err != nil {
		return SelectedSourceConnection{}, err
	}
	if conn.Status != "active" {
		return SelectedSourceConnection{}, fmt.Errorf("%w: %s", ErrSourceConnectionDisabled, connectionID)
	}
	plaintext := []byte(nil)
	if len(conn.CredentialsCiphertext) > 0 {
		plaintext, err = s.cipher.Decrypt(conn.ID, conn.CredentialsCiphertext)
		if err != nil {
			return SelectedSourceConnection{}, fmt.Errorf("解密来源连接凭证失败: %w", err)
		}
	}
	appID, _ := conn.Config["app_id"].(string)
	return SelectedSourceConnection{
		ConnectionID: conn.ID,
		WorkspaceID:  conn.WorkspaceID,
		Provider:     conn.Provider,
		AppID:        appID,
		AppSecret:    plaintext,
		Config:       conn.Config,
	}, nil
}
