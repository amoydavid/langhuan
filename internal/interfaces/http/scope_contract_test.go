package http

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"testing"

	"github.com/dajee/langhuan/internal/domain/value"
)

// 这组契约测试防止「后端权威 scope 集合」与「OpenAPI 文档 / 前端表单」漂移。
// 本轮 bug 的根因正是前端硬编码列表漏了 knowledge_bases:read，导致用户创建的 key
// 无法通过只读接口鉴权。这里用 fail-loud 的方式锁死三处定义必须一致。

// repoRoot 从测试文件位置上溯到仓库根，不依赖 go test 的工作目录假设。
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 失败，无法定位仓库根")
	}
	// internal/interfaces/http/scope_contract_test.go → 上溯 4 级到仓库根。
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("未在 %s 找到 go.mod，仓库根定位失败", root)
	}
	return root
}

// extractScopeLiterals 从 TS 源码片段里提取单引号包裹的 scope 字面量，去重保序。
// 仅匹配 'xxx:yyy' 形式，避免误捕类型注解或注释。
func extractScopeLiterals(t *testing.T, src string, source string) []value.APIScope {
	t.Helper()
	re := regexp.MustCompile(`'([a-z_]+:[a-z_]+)'`)
	matches := re.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		t.Fatalf("未从 %s 解析到任何 scope 字面量，文件结构是否变化？", source)
	}
	seen := make(map[value.APIScope]bool)
	out := make([]value.APIScope, 0, len(matches))
	for _, m := range matches {
		s := value.APIScope(m[1])
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// extractArrayBlock 从源码里按锚点定位数组块，覆盖两种 TS 形态：
//   - "anchor ... = [ ... ]"（display.ts 的 const apiKeyScopeOrder = [...]，
//     中间可能夹类型注解如 : readonly APIKeyScope[]，需用 = [ 精确定位到真正的赋值数组）
//   - "anchor([ ... ])"（schemas.ts 的 z.enum([...])）
//
// 取锚点之后首个匹配，[ ... ] 内部不嵌套 ]。
func extractArrayBlock(t *testing.T, src, anchor, source string) string {
	t.Helper()
	// 形态1：锚点后跨过任意字符直到 "= ["；形态2：锚点紧跟 "(" 再到 "["。
	re := regexp.MustCompile(regexp.QuoteMeta(anchor) + `(?:.*?=\s*\[|\s*\(\s*\[)([^\]]*)\]`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("未在 %s 中定位 %s 数组块，文件结构是否变化？", source, anchor)
	}
	return m[1]
}

// TestOpenAPIAPIScopeEnumMatchesAllAPIScopes 锁死后端内部契约：
// OpenAPI 反射出的 APIScope enum 必须等于 AllAPIScopes()。
func TestOpenAPIAPIScopeEnumMatchesAllAPIScopes(t *testing.T) {
	b := newSpecBuilder()
	ref, err := b.schemaRef(reflect.TypeOf(value.ScopeKnowledgeBasesRead))
	if err != nil {
		t.Fatalf("反射 value.APIScope 失败: %v", err)
	}
	if ref.Value == nil || len(ref.Value.Enum) == 0 {
		t.Fatal("value.APIScope 反射后 enum 为空，schemaCustomizer 未生效")
	}
	got := make([]value.APIScope, 0, len(ref.Value.Enum))
	for _, v := range ref.Value.Enum {
		s, ok := v.(value.APIScope)
		if !ok {
			t.Fatalf("enum 元素不是 value.APIScope，实际类型 %T", v)
		}
		got = append(got, s)
	}
	want := value.AllAPIScopes()
	if !slices.Equal(got, want) {
		t.Fatalf("OpenAPI APIScope enum = %v, want AllAPIScopes() = %v", got, want)
	}
}

// TestFrontendAPIScopeOrderMatchesAllAPIScopes 锁死前后端契约：
// 前端表单实际渲染的 scope 列表（display.ts 的 apiKeyScopeOrder）必须与后端权威源一致。
func TestFrontendAPIScopeOrderMatchesAllAPIScopes(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "web", "src", "features", "api-keys", "display.ts"))
	if err != nil {
		t.Fatalf("读取 display.ts 失败: %v", err)
	}
	block := extractArrayBlock(t, string(src), "apiKeyScopeOrder", "display.ts")
	got := extractScopeLiterals(t, block, "display.ts apiKeyScopeOrder")
	want := value.AllAPIScopes()
	if !slices.Equal(got, want) {
		t.Fatalf("前端 apiKeyScopeOrder = %v, want AllAPIScopes() = %v", got, want)
	}
}

// TestFrontendAPIScopeSchemaMatchesAllAPIScopes 锁死前后端契约：
// 前端 Zod 校验的 scope enum（schemas.ts）必须与后端权威源一致。
// 防止「表单能选、但提交被 Zod 拒」或「后端新增 scope、前端校验拒绝」。
func TestFrontendAPIScopeSchemaMatchesAllAPIScopes(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "web", "src", "features", "api-keys", "schemas.ts"))
	if err != nil {
		t.Fatalf("读取 schemas.ts 失败: %v", err)
	}
	// schemas.ts 里 scope enum 出现在 z.enum([...]) 中，直接在整个文件 scope 范围内提取。
	// 这里不锚定具体行，而是取所有 scope 字面量后与权威源比对（集合相等即可）。
	block := extractArrayBlock(t, string(src), "z.enum", "schemas.ts")
	got := extractScopeLiterals(t, block, "schemas.ts z.enum")
	// z.enum 的顺序无契约意义（只是合法值集合），按排序比对。
	wantSorted := append([]value.APIScope(nil), value.AllAPIScopes()...)
	sort.Slice(wantSorted, func(i, j int) bool { return wantSorted[i] < wantSorted[j] })
	gotSorted := append([]value.APIScope(nil), got...)
	sort.Slice(gotSorted, func(i, j int) bool { return gotSorted[i] < gotSorted[j] })
	if !slices.Equal(gotSorted, wantSorted) {
		t.Fatalf("前端 z.enum scope 集合 = %v, want AllAPIScopes() = %v", gotSorted, wantSorted)
	}
}
