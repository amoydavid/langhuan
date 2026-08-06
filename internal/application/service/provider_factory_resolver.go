package service

import (
	"encoding/json"

	"github.com/dajee/langhuan/internal/domain/value"
	modelcatalogport "github.com/dajee/langhuan/internal/ports/modelcatalog"
)

// ProviderDecodeResult 是 Provider 配置解码后的统一结果。
type ProviderDecodeResult struct {
	Config          map[string]any
	CredentialsJSON []byte
}

// ProviderFactoryInfo 是 Provider descriptor 对既有应用服务暴露的统一视图。
type ProviderFactoryInfo struct {
	ProviderName     string
	CredentialFields []string
	Capabilities     []value.ProviderCapability
	DecodeProvider   func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error)
	ModelCatalog     modelcatalogport.Catalog
}

// ProviderOption 描述一个 Provider 的 capability 视图。
type ProviderOption struct {
	Key          string
	Capabilities []value.ProviderCapability
	ModelCatalog bool
}

// ProviderFactoryResolver 按显式 descriptor 路由 Provider 配置能力。
type ProviderFactoryResolver struct {
	descriptors *ProviderDescriptorRegistry
}

// NewProviderFactoryResolver 创建 descriptor 驱动的 Provider 解析器。
func NewProviderFactoryResolver(descriptors *ProviderDescriptorRegistry) *ProviderFactoryResolver {
	return &ProviderFactoryResolver{descriptors: descriptors}
}

// SupportedProviders 返回已注册的 provider key。
func (r *ProviderFactoryResolver) SupportedProviders() []string {
	options := r.ProviderOptions()
	providers := make([]string, 0, len(options))
	for _, option := range options {
		providers = append(providers, option.Key)
	}
	return providers
}

// ProviderOptions 返回带 capability 的 provider 选项。
func (r *ProviderFactoryResolver) ProviderOptions() []ProviderOption {
	if r == nil {
		return nil
	}
	return r.descriptors.Options()
}

// Supports 判断指定 provider 是否已注册。
func (r *ProviderFactoryResolver) Supports(provider string) bool {
	_, err := r.Resolve(provider)
	return err == nil
}

// Resolve 返回显式注册的 Provider descriptor 统一视图。
func (r *ProviderFactoryResolver) Resolve(provider string) (ProviderFactoryInfo, error) {
	descriptor, err := r.descriptors.Descriptor(provider)
	if err != nil {
		return ProviderFactoryInfo{}, err
	}
	return ProviderFactoryInfo{
		ProviderName:     descriptor.Key,
		CredentialFields: descriptor.CredentialFields,
		Capabilities:     descriptor.Capabilities,
		DecodeProvider:   descriptor.DecodeProvider,
		ModelCatalog:     descriptor.ModelCatalog,
	}, nil
}
