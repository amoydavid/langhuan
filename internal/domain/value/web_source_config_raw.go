package value

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// 本文件承载 web_source_config 解析所需的原始值收窄辅助：把 jsonb 解码出的
// map[string]any（数值常为 float64）安全收窄为 Go 类型，统一 ErrValidation 语义。
// 类型形状与子对象解析器分别在 web_source_config.go / web_source_config_parse.go。

// webSourceObject 取子对象；null/缺省视为未设置（present=false），非对象报错。
// key 用于容器取值，errKey 是含父路径的完整点分路径（如 "extract.readability"），
// 与其它错误的字段路径约定一致。
func webSourceObject(container map[string]any, key, errKey string) (obj map[string]any, present bool, err error) {
	v, ok := container[key]
	if !ok || v == nil {
		return nil, false, nil
	}
	obj, ok = v.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("%w: web 源配置 %q 必须是对象", domainerrors.ErrValidation, errKey)
	}
	return obj, true, nil
}

// webSourceBool 读取可选布尔键；缺省/null 不改动 out（保留调用方预设的缺省值）。
func webSourceBool(m map[string]any, rawKey, errKey string, out *bool) error {
	v, present := m[rawKey]
	if !present || v == nil {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		return fmt.Errorf("%w: %s 必须是布尔值", domainerrors.ErrValidation, errKey)
	}
	*out = b
	return nil
}

// webSourceInt 把 JSON 数值（jsonb 解码常见 float64，兼容手工构造的 int 系与 json.Number）
// 收窄为 int；小数、超界与非数值报错。
func webSourceInt(v any, errKey string) (int, error) {
	invalid := func() (int, error) {
		return 0, fmt.Errorf("%w: %s 必须是整数", domainerrors.ErrValidation, errKey)
	}
	switch n := v.(type) {
	case float64:
		if n != math.Trunc(n) || math.Abs(n) >= 1<<53 {
			return invalid()
		}
		return int(n), nil
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return invalid()
		}
		return int(i), nil
	}
	return invalid()
}

// webSourceStringArray 收窄字符串数组；兼容 []any（JSON 解码）与 []string（手工构造）。
func webSourceStringArray(v any, errKey string) ([]string, error) {
	switch arr := v.(type) {
	case []any:
		out := make([]string, 0, len(arr))
		for i, item := range arr {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%w: %s 第 %d 项必须是字符串", domainerrors.ErrValidation, errKey, i)
			}
			out = append(out, s)
		}
		return out, nil
	case []string:
		return slices.Clone(arr), nil
	}
	return nil, fmt.Errorf("%w: %s 必须是字符串数组", domainerrors.ErrValidation, errKey)
}

// webSourceTrimmedNonEmpty 逐项去首尾空白并拒绝空白项（glob 路径与类名都不该含空白）。
func webSourceTrimmedNonEmpty(items []string, errKey string) ([]string, error) {
	out := make([]string, 0, len(items))
	for i, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			return nil, fmt.Errorf("%w: %s 第 %d 项不能为空", domainerrors.ErrValidation, errKey, i)
		}
		out = append(out, trimmed)
	}
	return out, nil
}

// rejectUnknownKeys 拒绝未知键（排序后报首个，保证错误信息确定）。
// scope 形如 "（crawl）"，顶层传空串。
func rejectUnknownKeys(m map[string]any, known []string, scope string) error {
	var unknown []string
	for key := range m {
		if !slices.Contains(known, key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%w: web 源配置%s存在未知键 %q", domainerrors.ErrValidation, scope, unknown[0])
}
