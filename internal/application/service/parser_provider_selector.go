package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

// ParserProviderSelector 为解析器运行时选择可用的 Parser Provider 凭据。
// 它从 model_providers 表查询 provider="mineru" 且 status="active" 的记录，
// workspace 级优先、回退 platform 级，返回解密后的配置与凭据供 ParserClient 构造。
type ParserProviderSelector struct {
	repository ModelProviderRepository
	cipher     credentialDecryptor
}

// credentialDecryptor 是 CredentialCipher 的最小子集，便于测试。
type credentialDecryptor interface {
	Decrypt(providerID uuid.UUID, ciphertext []byte) ([]byte, error)
}

// SelectedParserProvider 是选中的 Parser Provider 及其解密后的凭据。
type SelectedParserProvider struct {
	Provider        *model.ModelProvider
	CredentialsJSON []byte
}

// NewParserProviderSelector 创建凭据选择服务。
func NewParserProviderSelector(repository ModelProviderRepository, cipher credentialDecryptor) *ParserProviderSelector {
	return &ParserProviderSelector{repository: repository, cipher: cipher}
}

// SelectMinerU 选择一个可用的 MinerU Parser Provider。
// workspace 级优先；多个时取 created_at 最新；无可用时返回错误。
func (s *ParserProviderSelector) SelectMinerU(ctx context.Context, workspaceID uuid.UUID) (SelectedParserProvider, error) {
	providers, err := s.repository.ListVisible(ctx, workspaceID)
	if err != nil {
		return SelectedParserProvider{}, fmt.Errorf("查询 Parser Provider 失败: %w", err)
	}

	var candidates []*model.ModelProvider
	for _, p := range providers {
		if p.Provider != "mineru" || p.Status != "active" {
			continue
		}
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		return SelectedParserProvider{}, fmt.Errorf("没有可用的 MinerU Parser Provider")
	}

	// 选择 created_at 最新的
	selected := candidates[0]
	for _, c := range candidates[1:] {
		if c.CreatedAt.After(selected.CreatedAt) {
			selected = c
		}
	}

	creds, err := s.cipher.Decrypt(selected.ID, selected.CredentialsCiphertext)
	if err != nil {
		return SelectedParserProvider{}, fmt.Errorf("解密 MinerU 凭据失败: %w", err)
	}

	return SelectedParserProvider{
		Provider:        selected,
		CredentialsJSON: creds,
	}, nil
}
