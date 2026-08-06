package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	modelcatalogport "github.com/dajee/langhuan/internal/ports/modelcatalog"
	parserproviderport "github.com/dajee/langhuan/internal/ports/parserprovider"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// ProviderDescriptor 描述一条 Provider 连接支持的全部能力及共享配置解码器。
type ProviderDescriptor struct {
	Key              string
	Capabilities     []value.ProviderCapability
	CredentialFields []string
	DecodeProvider   func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error)
	ModelCatalog     modelcatalogport.Catalog
}

// ProviderDescriptorRegistry 保存按 provider key 唯一注册的显式描述符。
type ProviderDescriptorRegistry struct {
	descriptors map[string]ProviderDescriptor
}

// NewProviderDescriptorRegistry 校验并注册 Provider 描述符。
func NewProviderDescriptorRegistry(descriptors ...ProviderDescriptor) (*ProviderDescriptorRegistry, error) {
	registry := &ProviderDescriptorRegistry{descriptors: make(map[string]ProviderDescriptor, len(descriptors))}
	for _, descriptor := range descriptors {
		normalized, err := normalizeProviderDescriptor(descriptor)
		if err != nil {
			return nil, err
		}
		if _, exists := registry.descriptors[normalized.Key]; exists {
			return nil, fmt.Errorf("Provider descriptor 重复: %s", normalized.Key)
		}
		registry.descriptors[normalized.Key] = normalized
	}
	return registry, nil
}

func normalizeProviderDescriptor(descriptor ProviderDescriptor) (ProviderDescriptor, error) {
	descriptor.Key = strings.ToLower(strings.TrimSpace(descriptor.Key))
	if descriptor.Key == "" {
		return ProviderDescriptor{}, fmt.Errorf("Provider descriptor key 不能为空")
	}
	if descriptor.DecodeProvider == nil {
		return ProviderDescriptor{}, fmt.Errorf("Provider descriptor %s 缺少配置解码器", descriptor.Key)
	}
	if len(descriptor.Capabilities) == 0 {
		return ProviderDescriptor{}, fmt.Errorf("Provider descriptor %s 至少需要一个 capability", descriptor.Key)
	}
	capabilities := make(map[value.ProviderCapability]struct{}, len(descriptor.Capabilities))
	for _, capability := range descriptor.Capabilities {
		normalizedCapability, valid := value.NormalizeProviderCapability(capability)
		if !valid {
			return ProviderDescriptor{}, fmt.Errorf("Provider descriptor %s 包含无效 capability: %s", descriptor.Key, capability)
		}
		capabilities[normalizedCapability] = struct{}{}
	}
	descriptor.Capabilities = make([]value.ProviderCapability, 0, len(capabilities))
	for capability := range capabilities {
		descriptor.Capabilities = append(descriptor.Capabilities, capability)
	}
	sort.Slice(descriptor.Capabilities, func(i, j int) bool {
		return descriptor.Capabilities[i] < descriptor.Capabilities[j]
	})
	descriptor.CredentialFields = append([]string(nil), descriptor.CredentialFields...)
	return descriptor, nil
}

// Descriptor 返回规范化后的 Provider 描述符。
func (r *ProviderDescriptorRegistry) Descriptor(provider string) (ProviderDescriptor, error) {
	key := strings.ToLower(strings.TrimSpace(provider))
	if r == nil {
		return ProviderDescriptor{}, fmt.Errorf("%w: %s", domainerrors.ErrUnsupportedProvider, key)
	}
	descriptor, ok := r.descriptors[key]
	if !ok {
		return ProviderDescriptor{}, fmt.Errorf("%w: %s", domainerrors.ErrUnsupportedProvider, key)
	}
	descriptor.Capabilities = append([]value.ProviderCapability(nil), descriptor.Capabilities...)
	descriptor.CredentialFields = append([]string(nil), descriptor.CredentialFields...)
	return descriptor, nil
}

// Options 返回按 provider key 稳定排序的能力选项。
func (r *ProviderDescriptorRegistry) Options() []ProviderOption {
	if r == nil {
		return nil
	}
	keys := make([]string, 0, len(r.descriptors))
	for key := range r.descriptors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	options := make([]ProviderOption, 0, len(keys))
	for _, key := range keys {
		descriptor := r.descriptors[key]
		options = append(options, ProviderOption{
			Key:          key,
			Capabilities: append([]value.ProviderCapability(nil), descriptor.Capabilities...),
			ModelCatalog: descriptor.ModelCatalog != nil,
		})
	}
	return options
}

// SupportsModelType 判断 Provider 是否显式声明支持该模型类型。
func (r *ProviderDescriptorRegistry) SupportsModelType(provider string, modelType value.ModelType) bool {
	capability := value.CapabilityFromModelType(modelType)
	if _, valid := value.NormalizeProviderCapability(capability); !valid {
		return false
	}
	descriptor, err := r.Descriptor(provider)
	if err != nil {
		return false
	}
	for _, supported := range descriptor.Capabilities {
		if supported == capability {
			return true
		}
	}
	return false
}

// EmbeddingProviderDescriptor 把单能力 Embedding Factory 适配成显式描述符。
func EmbeddingProviderDescriptor(factory embeddingport.Factory) ProviderDescriptor {
	descriptor := ProviderDescriptor{
		Key:              factory.Provider(),
		Capabilities:     []value.ProviderCapability{value.CapabilityEmbedding},
		CredentialFields: factory.CredentialFields(),
		DecodeProvider: func(scope value.ModelScope, config, credentials json.RawMessage) (ProviderDecodeResult, error) {
			decoded, credentialsJSON, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{
				Scope: scope, Config: config, Credentials: credentials,
			})
			return ProviderDecodeResult{Config: decoded, CredentialsJSON: credentialsJSON}, err
		},
	}
	if catalogProvider, ok := factory.(modelcatalogport.CatalogProvider); ok {
		descriptor.ModelCatalog = catalogProvider.ModelCatalog()
	}
	return descriptor
}

// RerankProviderDescriptor 把单能力 Rerank Factory 适配成显式描述符。
func RerankProviderDescriptor(factory rerankport.Factory) ProviderDescriptor {
	descriptor := ProviderDescriptor{
		Key:              factory.Provider(),
		Capabilities:     []value.ProviderCapability{value.CapabilityRerank},
		CredentialFields: factory.CredentialFields(),
		DecodeProvider: func(scope value.ModelScope, config, credentials json.RawMessage) (ProviderDecodeResult, error) {
			decoded, credentialsJSON, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
				Scope: scope, Config: config, Credentials: credentials,
			})
			return ProviderDecodeResult{Config: decoded, CredentialsJSON: credentialsJSON}, err
		},
	}
	if catalogProvider, ok := factory.(modelcatalogport.CatalogProvider); ok {
		descriptor.ModelCatalog = catalogProvider.ModelCatalog()
	}
	return descriptor
}

// ParserProviderDescriptor 把单能力 Parser Factory 适配成显式描述符。
func ParserProviderDescriptor(factory parserproviderport.Factory) ProviderDescriptor {
	return ProviderDescriptor{
		Key:              factory.Provider(),
		Capabilities:     []value.ProviderCapability{value.CapabilityParser},
		CredentialFields: factory.CredentialFields(),
		DecodeProvider: func(scope value.ModelScope, config, credentials json.RawMessage) (ProviderDecodeResult, error) {
			decoded, credentialsJSON, err := factory.DecodeProvider(parserproviderport.ProviderDecodeInput{
				Scope: scope, Config: config, Credentials: credentials,
			})
			return ProviderDecodeResult{Config: decoded, CredentialsJSON: credentialsJSON}, err
		},
	}
}
