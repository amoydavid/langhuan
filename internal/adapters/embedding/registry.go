package embedding

import (
	"fmt"
	"reflect"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

type registryKey struct {
	modelType value.ModelType
	provider  string
}

// Registry 保存进程内已注册的 Provider Factory。
type Registry struct {
	factories map[registryKey]embeddingport.Factory
}

// NewRegistry 创建只注册 Embedding 能力的 Factory Registry。
func NewRegistry(factories ...embeddingport.Factory) (*Registry, error) {
	registry := &Registry{factories: make(map[registryKey]embeddingport.Factory, len(factories))}
	for _, factory := range factories {
		if factoryIsNil(factory) {
			return nil, fmt.Errorf("注册 Embedding Factory 失败: factory 不能为空")
		}
		provider := strings.ToLower(strings.TrimSpace(factory.Provider()))
		if provider == "" {
			return nil, fmt.Errorf("注册 Embedding Factory 失败: provider 不能为空")
		}
		key := registryKey{modelType: value.ModelTypeEmbedding, provider: provider}
		if _, exists := registry.factories[key]; exists {
			return nil, fmt.Errorf("注册 Embedding Factory 失败: provider %q 重复", provider)
		}
		registry.factories[key] = factory
	}
	return registry, nil
}

// Factory 返回指定组合的 Factory；v0.3.1 只支持 embedding。
func (r *Registry) Factory(modelType value.ModelType, provider string) (embeddingport.Factory, error) {
	if modelType != value.ModelTypeEmbedding {
		return nil, fmt.Errorf("%w: %s", domainerrors.ErrUnsupportedModelType, modelType)
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	factory, ok := r.factories[registryKey{modelType: modelType, provider: provider}]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domainerrors.ErrUnsupportedProvider, provider)
	}
	return factory, nil
}

// Factories 返回已注册的全部 Embedding Factory，供装配层枚举可用 provider。
func (r *Registry) Factories() []embeddingport.Factory {
	result := make([]embeddingport.Factory, 0, len(r.factories))
	for _, factory := range r.factories {
		result = append(result, factory)
	}
	return result
}

func factoryIsNil(factory embeddingport.Factory) bool {
	if factory == nil {
		return true
	}
	valueOf := reflect.ValueOf(factory)
	switch valueOf.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return valueOf.IsNil()
	default:
		return false
	}
}
