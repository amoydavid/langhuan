// Package parserprovider 定义解析器 Provider（如 MinerU Cloud）的凭据解码与客户端构造能力。
// 它与 embedding 能力域完全解耦：embedding Provider 走 ports/embedding.Factory，
// parser Provider 走本包的 Factory。两者共享 model_providers 表和 CredentialCipher 加密机制。
package parserprovider

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// ProviderDecodeInput 保留严格 JSON 的"缺失/空对象"语义，交给 Provider 专属 codec 解码。
type ProviderDecodeInput struct {
	Scope       value.ModelScope
	Config      json.RawMessage
	Credentials json.RawMessage
}

// ClientInput 是构造短生命周期 ParserClient 所需的规范化持久化数据。
type ClientInput struct {
	ProviderID      uuid.UUID
	Scope           value.ModelScope
	Config          map[string]any
	CredentialsJSON []byte
}

// ParserClient 是解析器客户端的最小接口，由具体 Provider 适配器实现。
// MinerU 的实现包含 RequestUploadURL / Upload / Poll / Download 等方法。
type ParserClient interface {
	// 具体方法在适配器层定义，这里只做类型锚点
}

// Factory 负责一个 Parser Provider 的 typed decode、校验和客户端构造。
type Factory interface {
	// Provider 返回 Provider 标识符（如 "mineru"）。
	Provider() string
	// CredentialFields 返回该 Provider 需要的凭据字段名列表，供前端表单渲染。
	CredentialFields() []string
	// DecodeProvider 校验并规范化 Provider 配置与凭据，返回非敏感 config map 与待加密的 credentials JSON。
	DecodeProvider(ProviderDecodeInput) (config map[string]any, credentialsJSON []byte, err error)
	// NewClient 用解密后的配置与凭据构造 ParserClient。
	NewClient(ctx context.Context, input ClientInput) (ParserClient, error)
}

// FactoryRegistry 使用 provider 名称查找 Parser Factory。
type FactoryRegistry interface {
	Factory(provider string) (Factory, error)
}
