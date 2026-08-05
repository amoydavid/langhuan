package service

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// ResolvedRerankClient 是校验通过后的运行时 Rerank 客户端及其不可变模型事实。
// ModelConfigHash 用于在检索时与 Generation 快照对比，检测配置漂移。
type ResolvedRerankClient struct {
	Client              rerankport.Client
	ModelID, ProviderID uuid.UUID
	ProviderKey         string
	ModelName           string
	ModelConfigHash     string
	MaxDocuments        int
	MaxQueryChars       int
	MaxDocumentChars    int
}

// RerankClientResolver 解析当前 Workspace 可见、Provider/Model active、
// type=rerank 的模型，构造运行时 Rerank 客户端。
type RerankClientResolver interface {
	Resolve(context.Context, uuid.UUID, uuid.UUID) (*ResolvedRerankClient, error)
}

type rerankClientResolver struct {
	models   ModelRepository
	cipher   embeddingport.CredentialCipher
	registry rerankport.FactoryRegistry
}

// NewRerankClientResolver 创建共享的运行时 Rerank 解析器。
func NewRerankClientResolver(
	models ModelRepository,
	cipher embeddingport.CredentialCipher,
	registry rerankport.FactoryRegistry,
) RerankClientResolver {
	return &rerankClientResolver{models: models, cipher: cipher, registry: registry}
}

func (r *rerankClientResolver) Resolve(ctx context.Context, workspaceID, modelID uuid.UUID) (*ResolvedRerankClient, error) {
	if workspaceID == uuid.Nil || modelID == uuid.Nil {
		return nil, fmt.Errorf("%w: Workspace/Model ID 不能为空", domainerrors.ErrValidation)
	}
	resolved, err := r.models.GetVisible(ctx, workspaceID, modelID)
	if err != nil {
		return nil, err
	}
	return buildResolvedRerankClient(ctx, resolved, r.cipher, r.registry, true)
}

// buildResolvedRerankClient 解密凭证并构造 Rerank 客户端，requireActive 控制是否强制 active。
func buildResolvedRerankClient(
	ctx context.Context,
	resolved *model.ResolvedModel,
	cipher embeddingport.CredentialCipher,
	registry rerankport.FactoryRegistry,
	requireActive bool,
) (*ResolvedRerankClient, error) {
	if resolved == nil || resolved.Model == nil || resolved.Provider == nil ||
		resolved.Model.ID == uuid.Nil || resolved.Provider.ID == uuid.Nil {
		return nil, fmt.Errorf("%w: Model/Provider 聚合无效", domainerrors.ErrValidation)
	}
	if resolved.Model.Type != value.ModelTypeRerank {
		return nil, domainerrors.ErrUnsupportedModelType
	}
	if requireActive {
		if resolved.Provider.Status != value.ModelStatusActive {
			return nil, domainerrors.ErrProviderDisabled
		}
		if resolved.Model.Status != value.ModelStatusActive {
			return nil, domainerrors.ErrModelDisabled
		}
	}
	maxDocuments, err := rerankIntParameter(resolved.Model.Parameters, "max_documents")
	if err != nil {
		return nil, err
	}
	maxQueryChars, err := rerankIntParameter(resolved.Model.Parameters, "max_query_chars")
	if err != nil {
		return nil, err
	}
	maxDocumentChars, err := rerankIntParameter(resolved.Model.Parameters, "max_document_chars")
	if err != nil {
		return nil, err
	}
	plaintext, err := cipher.Decrypt(resolved.Provider.ID, resolved.Provider.CredentialsCiphertext)
	if err != nil {
		return nil, fmt.Errorf("Provider %s: %w", resolved.Provider.ID, domainerrors.ErrCredentialDecryption)
	}
	defer clearBytes(plaintext)
	factory, err := registry.Factory(resolved.Provider.Provider)
	if err != nil {
		return nil, err
	}
	client, err := factory.NewClient(ctx, rerankport.ClientInput{
		ProviderID:      resolved.Provider.ID,
		Scope:           resolved.Provider.Scope,
		Config:          resolved.Provider.Config,
		CredentialsJSON: plaintext,
		ModelName:       resolved.Model.ModelName,
		Parameters:      resolved.Model.Parameters,
	})
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("%w: Rerank 客户端为空", domainerrors.ErrInvalidRerankResponse)
	}
	configHash, err := rerankModelConfigHash(resolved)
	if err != nil {
		return nil, err
	}
	return &ResolvedRerankClient{
		Client:           client,
		ModelID:          resolved.Model.ID,
		ProviderID:       resolved.Provider.ID,
		ProviderKey:      resolved.Provider.Provider,
		ModelName:        resolved.Model.ModelName,
		ModelConfigHash:  configHash,
		MaxDocuments:     maxDocuments,
		MaxQueryChars:    maxQueryChars,
		MaxDocumentChars: maxDocumentChars,
	}, nil
}

// rerankIntParameter 把持久化 map 中的数值字段解析为 int，严格拒绝缺失/类型错误/非整数。
func rerankIntParameter(parameters map[string]any, key string) (int, error) {
	raw, ok := parameters[key]
	if !ok {
		return 0, fmt.Errorf("%w: %s 缺失", domainerrors.ErrInvalidProviderConfig, key)
	}
	switch value := raw.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		if math.Trunc(value) != value {
			return 0, fmt.Errorf("%w: %s 必须是整数", domainerrors.ErrInvalidProviderConfig, key)
		}
		return int(value), nil
	default:
		return 0, fmt.Errorf("%w: %s 类型无效", domainerrors.ErrInvalidProviderConfig, key)
	}
}
