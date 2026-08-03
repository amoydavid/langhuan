package mcp

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

// uuidStringSchema 把 uuid.UUID 声明为 JSON string。
//
// uuid.UUID 的底层类型是 [16]byte（reflect.Array），jsonschema-go 默认会把它推导成
// "type":"array"，但 uuid.UUID 的 MarshalJSON 实际输出字符串。这与 time.Time（底层
// 是 struct 但序列化为 string）是同一类问题。库自身用 initialSchemaMap 把 time.Time
// 登记为 string；这里在调用侧用 ForOptions.TypeSchemas 做等价的覆盖。
var uuidStringSchema = &jsonschema.Schema{Type: "string"}

// schemaOverrides 登记需要覆盖 jsonschema-go 默认推导的 Go 类型。
//
// 目前包含：
//   - uuid.UUID：序列化为字符串，schema 应为 string 而非 array。
//
// 只登记值类型即可：jsonschema-go 的 forType 会先解引用指针再查表，并对指针自动
// 追加 "null" 联合类型（见 infer.go 的 allowNull 处理），因此 *uuid.UUID 无需单独登记。
var schemaOverrides = map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[uuid.UUID](): uuidStringSchema,
}

// schemaOpts 是所有 MCP 工具 schema 生成共用的选项：
//
//   - IgnoreInvalidTypes：与 mcp-go 自带的 schemaFor 行为一致，遇到无法表达的字段
//     （如 chan/func）时跳过而非报错，避免单个字段阻塞整个工具注册。
//   - TypeSchemas：uuid.UUID → string 的全局映射，一处声明、所有工具生效。
var schemaOpts = &jsonschema.ForOptions{
	IgnoreInvalidTypes: true,
	TypeSchemas:        schemaOverrides,
}

// rawSchema 基于 Go 类型 T 生成 JSON Schema 并序列化为 json.RawMessage，
// 供 mcp.WithRawInputSchema / mcp.WithRawOutputSchema 注册。
//
// 相比 mcp-go 自带的 WithInputSchema[T]()/WithOutputSchema[T]()（内部写死 opts、
// 不支持 TypeSchemas），这里显式调用 jsonschema.For[T] 并注入 uuid.UUID→string 映射，
// 使 DTO 中的 uuid.UUID 字段被正确声明为 string，与 MarshalJSON 输出一致。
//
// jsonschema-go 原生支持 "jsonschema" struct tag（作为 description），因此现有
// 工具 DTO 的 tag 约定无需额外处理。
func rawSchema[T any]() (json.RawMessage, error) {
	schema, err := jsonschema.For[T](schemaOpts)
	if err != nil {
		return nil, fmt.Errorf("生成 schema: %w", err)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("序列化 schema: %w", err)
	}
	return raw, nil
}

// rawInputSchema 生成工具输入 schema。
func rawInputSchema[T any]() json.RawMessage {
	return mustRawSchema[T]()
}

// rawOutputSchema 生成工具输出 schema。
func rawOutputSchema[T any]() json.RawMessage {
	return mustRawSchema[T]()
}

// mustRawSchema 在包初始化/工具注册期生成 schema，失败直接 panic：
// schema 生成失败属于编程期错误（类型定义问题），不应推迟到运行期。
func mustRawSchema[T any]() json.RawMessage {
	raw, err := rawSchema[T]()
	if err != nil {
		panic(err)
	}
	return raw
}
