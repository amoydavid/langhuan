package rerank

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// ProviderDecodeInput 保留严格 JSON 的“缺失/空对象”语义，交给 Provider 专属 codec 解码。
type ProviderDecodeInput struct {
	Scope       value.ModelScope
	Config      json.RawMessage
	Credentials json.RawMessage
}

// ModelDecodeInput 描述具体模型及其 Provider 专属 parameters。
type ModelDecodeInput struct {
	ModelName  string
	Parameters json.RawMessage
}

// ClientInput 是构造短生命周期 RerankClient 所需的规范化持久化数据。
type ClientInput struct {
	ProviderID      uuid.UUID
	Scope           value.ModelScope
	Config          map[string]any
	CredentialsJSON []byte
	ModelName       string
	Parameters      map[string]any
}

// Factory 负责一个原生 Rerank Provider 的 typed decode、校验和客户端构造。
type Factory interface {
	Provider() string
	CredentialFields() []string
	DecodeProvider(ProviderDecodeInput) (config map[string]any, credentialsJSON []byte, err error)
	DecodeModel(ModelDecodeInput) (parameters map[string]any, err error)
	NewClient(context.Context, ClientInput) (Client, error)
}

// FactoryRegistry 使用 provider key 查找 Factory。Rerank 只支持单一能力，
// 因此查找键只包含 provider，不再携带 model_type。
type FactoryRegistry interface {
	Factory(provider string) (Factory, error)
}
