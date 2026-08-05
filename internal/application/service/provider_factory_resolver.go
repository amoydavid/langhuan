package service

import (
	"encoding/json"
	"fmt"
	"sort"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	parserproviderport "github.com/dajee/langhuan/internal/ports/parserprovider"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// ProviderDecodeResult 是 Provider 配置解码后的统一结果。
type ProviderDecodeResult struct {
	Config          map[string]any
	CredentialsJSON []byte
}

// ProviderFactoryInfo 是 Provider Factory 的统一视图，
// 屏蔽 embedding/rerank/parser Factory 的类型差异。
type ProviderFactoryInfo struct {
	ProviderName     string
	CredentialFields []string
	Capabilities     []value.ProviderCapability
	// DecodeProvider 校验并规范化 Provider 配置与凭据。
	DecodeProvider func(scope value.ModelScope, config, credentials json.RawMessage) (ProviderDecodeResult, error)
}

// ProviderFactoryResolver 按 provider 名称路由到对应能力域的 Factory。
// 它依次查 embedding、rerank、parser registry，都不命中才报错。
type ProviderFactoryResolver struct {
	embeddingRegistry embeddingport.FactoryRegistry
	rerankRegistry    rerankport.FactoryRegistry
	parserRegistry    *ParserRegistryAdapter
	// supportedProviders 是装配时固定的可用 provider 键集合，
	// 供 Web Console 渲染 Provider 选项（如 mineru 仅在启用时可用）。
	supportedProviders []string
}

// ParserRegistryAdapter 把 parserprovider.FactoryRegistry 适配为
// ProviderFactoryResolver 可用的形式。
type ParserRegistryAdapter struct {
	registry parserproviderport.FactoryRegistry
}

// NewParserRegistryAdapter 包装 parserprovider.FactoryRegistry。
func NewParserRegistryAdapter(registry parserproviderport.FactoryRegistry) *ParserRegistryAdapter {
	return &ParserRegistryAdapter{registry: registry}
}

// Supports 判断 registry 是否包含指定 provider。
func (a *ParserRegistryAdapter) Supports(provider string) bool {
	_, err := a.registry.Factory(provider)
	return err == nil
}

// Factory 返回指定 provider 的 parser Factory。
func (a *ParserRegistryAdapter) Factory(provider string) (parserproviderport.Factory, error) {
	return a.registry.Factory(provider)
}

// NewProviderFactoryResolver 创建一个同时覆盖 embedding、rerank 和 parser 能力域的解析器。
// parserAdapter 可为 nil（表示进程未注册任何 parser provider）。
// supportedProviders 为装配时确定的可用 provider 键集合，供前端渲染选项。
func NewProviderFactoryResolver(
	embeddingRegistry embeddingport.FactoryRegistry,
	rerankRegistry rerankport.FactoryRegistry,
	parserAdapter *ParserRegistryAdapter,
	supportedProviders ...string,
) *ProviderFactoryResolver {
	return &ProviderFactoryResolver{
		embeddingRegistry:  embeddingRegistry,
		rerankRegistry:     rerankRegistry,
		parserRegistry:     parserAdapter,
		supportedProviders: append([]string(nil), supportedProviders...),
	}
}

// SupportedProviders 返回装配时可用的 provider 键列表（如 openai/ark/ollama/dashscope/tencentcloud/rerank_compatible/mineru）。
// 用于 Web Console 渲染 Provider 选择下拉，避免展示不可用的选项。
func (r *ProviderFactoryResolver) SupportedProviders() []string {
	return append([]string(nil), r.supportedProviders...)
}

// ProviderOptions 返回带 capability 的 provider 选项，按 key 稳定排序。
// 供 Web Console 渲染“每个 Provider 支持哪些模型类型”。
func (r *ProviderFactoryResolver) ProviderOptions() []ProviderOption {
	options := make(map[string][]value.ProviderCapability, len(r.supportedProviders))
	for _, provider := range r.supportedProviders {
		info, err := r.Resolve(provider)
		if err != nil {
			continue
		}
		options[provider] = info.Capabilities
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]ProviderOption, 0, len(keys))
	for _, key := range keys {
		result = append(result, ProviderOption{Key: key, Capabilities: options[key]})
	}
	return result
}

// ProviderOption 描述一个 Provider 的 capability 视图。
type ProviderOption struct {
	Key          string
	Capabilities []value.ProviderCapability
}

// Supports 判断指定 provider 是否已注册（可用）。
func (r *ProviderFactoryResolver) Supports(provider string) bool {
	_, err := r.Resolve(provider)
	return err == nil
}

// Resolve 按 provider 名称查找 Factory，返回统一视图。
// 依次查 embedding、rerank、parser registry。
func (r *ProviderFactoryResolver) Resolve(provider string) (ProviderFactoryInfo, error) {
	// 尝试 embedding registry
	embFactory, embErr := r.embeddingRegistry.Factory(value.ModelTypeEmbedding, provider)
	if embErr == nil {
		return ProviderFactoryInfo{
			ProviderName:     embFactory.Provider(),
			CredentialFields: embFactory.CredentialFields(),
			Capabilities:     []value.ProviderCapability{value.CapabilityEmbedding},
			DecodeProvider: func(scope value.ModelScope, config, credentials json.RawMessage) (ProviderDecodeResult, error) {
				cfg, credJSON, err := embFactory.DecodeProvider(embeddingport.ProviderDecodeInput{
					Scope:       scope,
					Config:      config,
					Credentials: credentials,
				})
				if err != nil {
					return ProviderDecodeResult{}, err
				}
				return ProviderDecodeResult{Config: cfg, CredentialsJSON: credJSON}, nil
			},
		}, nil
	}

	// 尝试 rerank registry
	if r.rerankRegistry != nil {
		rerankFactory, rrErr := r.rerankRegistry.Factory(provider)
		if rrErr == nil {
			return ProviderFactoryInfo{
				ProviderName:     rerankFactory.Provider(),
				CredentialFields: rerankFactory.CredentialFields(),
				Capabilities:     []value.ProviderCapability{value.CapabilityRerank},
				DecodeProvider: func(scope value.ModelScope, config, credentials json.RawMessage) (ProviderDecodeResult, error) {
					cfg, credJSON, err := rerankFactory.DecodeProvider(rerankport.ProviderDecodeInput{
						Scope:       scope,
						Config:      config,
						Credentials: credentials,
					})
					if err != nil {
						return ProviderDecodeResult{}, err
					}
					return ProviderDecodeResult{Config: cfg, CredentialsJSON: credJSON}, nil
				},
			}, nil
		}
	}

	// 尝试 parser registry
	if r.parserRegistry != nil {
		parserFactory, pErr := r.parserRegistry.Factory(provider)
		if pErr == nil {
			return ProviderFactoryInfo{
				ProviderName:     parserFactory.Provider(),
				CredentialFields: parserFactory.CredentialFields(),
				Capabilities:     []value.ProviderCapability{value.CapabilityParser},
				DecodeProvider: func(scope value.ModelScope, config, credentials json.RawMessage) (ProviderDecodeResult, error) {
					cfg, credJSON, err := parserFactory.DecodeProvider(parserproviderport.ProviderDecodeInput{
						Scope:       scope,
						Config:      config,
						Credentials: credentials,
					})
					if err != nil {
						return ProviderDecodeResult{}, err
					}
					return ProviderDecodeResult{Config: cfg, CredentialsJSON: credJSON}, nil
				},
			}, nil
		}
	}

	return ProviderFactoryInfo{}, fmt.Errorf("%w: %s", domainerrors.ErrUnsupportedProvider, provider)
}
