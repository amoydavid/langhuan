// Package parserprovider 提供 Parser Provider Factory 的进程内注册与查找。
package parserprovider

import (
	"fmt"
	"reflect"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	parserproviderport "github.com/dajee/langhuan/internal/ports/parserprovider"
)

// Registry 保存进程内已注册的 Parser Provider Factory。
type Registry struct {
	factories map[string]parserproviderport.Factory
}

// NewRegistry 创建 Parser Factory Registry。
func NewRegistry(factories ...parserproviderport.Factory) (*Registry, error) {
	registry := &Registry{factories: make(map[string]parserproviderport.Factory, len(factories))}
	for _, factory := range factories {
		if factoryIsNil(factory) {
			return nil, fmt.Errorf("注册 Parser Factory 失败: factory 不能为空")
		}
		provider := strings.ToLower(strings.TrimSpace(factory.Provider()))
		if provider == "" {
			return nil, fmt.Errorf("注册 Parser Factory 失败: provider 不能为空")
		}
		if _, exists := registry.factories[provider]; exists {
			return nil, fmt.Errorf("注册 Parser Factory 失败: provider %q 重复", provider)
		}
		registry.factories[provider] = factory
	}
	return registry, nil
}

// Factory 返回指定 provider 的 Factory。
func (r *Registry) Factory(provider string) (parserproviderport.Factory, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	factory, ok := r.factories[provider]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domainerrors.ErrUnsupportedProvider, provider)
	}
	return factory, nil
}

// Supports 判断 registry 是否包含指定 provider，供 ModelProviderService 路由使用。
func (r *Registry) Supports(provider string) bool {
	_, err := r.Factory(provider)
	return err == nil
}

// Factories 返回已注册的全部 Factory，供装配层枚举可用 provider 键。
func (r *Registry) Factories() []parserproviderport.Factory {
	result := make([]parserproviderport.Factory, 0, len(r.factories))
	for _, factory := range r.factories {
		result = append(result, factory)
	}
	return result
}

func factoryIsNil(factory parserproviderport.Factory) bool {
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
