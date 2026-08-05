// Package rerank 实现琅嬛与外部 Rerank 协议之间的适配层，包含 Factory Registry、
// 稳定的供应商错误清洗和具体 Provider（如 rerank_compatible）的实现。
package rerank

import (
	"fmt"
	"reflect"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// Registry 保存进程内已注册的 Rerank Provider Factory。
type Registry struct {
	factories map[string]rerankport.Factory
}

// NewRegistry 创建只注册 Rerank 能力的 Factory Registry。provider key 会做小写 trim
// 归一化，nil、空 key 与重复 key 一律注册失败。
func NewRegistry(factories ...rerankport.Factory) (*Registry, error) {
	registry := &Registry{factories: make(map[string]rerankport.Factory, len(factories))}
	for _, factory := range factories {
		if factoryIsNil(factory) {
			return nil, fmt.Errorf("注册 Rerank Factory 失败: factory 不能为空")
		}
		provider := normalizeProvider(factory.Provider())
		if provider == "" {
			return nil, fmt.Errorf("注册 Rerank Factory 失败: provider 不能为空")
		}
		if _, exists := registry.factories[provider]; exists {
			return nil, fmt.Errorf("注册 Rerank Factory 失败: provider %q 重复", provider)
		}
		registry.factories[provider] = factory
	}
	return registry, nil
}

// Factory 返回指定 provider 的 Factory；provider key 不区分大小写与首尾空白。
func (r *Registry) Factory(provider string) (rerankport.Factory, error) {
	provider = normalizeProvider(provider)
	factory, ok := r.factories[provider]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domainerrors.ErrUnsupportedProvider, provider)
	}
	return factory, nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func factoryIsNil(factory rerankport.Factory) bool {
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
