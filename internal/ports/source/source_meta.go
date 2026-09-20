package source

import (
	"time"
)

// documents.source_meta 的键清单（spec §4.3）。
//
// 这六个键在 web 抓取链路上被三处消费——web adapter 写入（http/render/provider 引擎）、
// application 层读写透传（源同步编排）、Console 展示；集中定义在此以避免字符串字面量散落。
// 放 ports 而非 domain model：source_meta 是存储形状（documents.source_meta jsonb 的键名），
// domain 不应承载存储细节。
const (
	// SourceMetaETag 是 http 引擎最近一次响应的 ETag，用于下次条件请求（If-None-Match）。
	SourceMetaETag = "etag"
	// SourceMetaLastModified 是 http 引擎最近一次响应的 Last-Modified 原值，
	// 用于下次条件请求（If-Modified-Since）；render/provider 通道不写。
	SourceMetaLastModified = "last_modified"
	// SourceMetaLastEngine 是实际执行引擎标识（http/render/firecrawl/jina），
	// 供 Console 展示与 auto 升级统计。
	SourceMetaLastEngine = "last_engine"
	// SourceMetaLastFetchAt 是最近一次抓取时间，编码为 RFC3339 UTC 字符串。
	SourceMetaLastFetchAt = "last_fetch_at"
	// SourceMetaLastHTTPStatus 是最近一次抓取的 HTTP 状态码（provider 引擎为等价语义值）。
	SourceMetaLastHTTPStatus = "last_http_status"
	// SourceMetaFinalURL 是重定向归并后的最终 URL。
	SourceMetaFinalURL = "final_url"
)

// SourceMeta 是 documents.source_meta（jsonb）的存储形状（spec §4.3）。
//
// 非 web 文档的 source_meta 保持 '{}'；本结构只描述 web 页面文档的键集。
type SourceMeta struct {
	ETag           string
	LastModified   string
	LastEngine     string
	LastFetchAt    time.Time
	LastHTTPStatus int
	FinalURL       string
}

// ToMap 把 SourceMeta 编码为写入 jsonb 的形状；零值字段省略，避免用空值覆盖
// 已存在的条件请求凭据。因此「读取旧值 → 合并本次结果 → 写回」的调用方必须自行
// 补全需要保留的字段（本方法只表达"本次确实观测到的值"）。
func (m SourceMeta) ToMap() map[string]any {
	out := make(map[string]any, 6)
	if m.ETag != "" {
		out[SourceMetaETag] = m.ETag
	}
	if m.LastModified != "" {
		out[SourceMetaLastModified] = m.LastModified
	}
	if m.LastEngine != "" {
		out[SourceMetaLastEngine] = m.LastEngine
	}
	if !m.LastFetchAt.IsZero() {
		out[SourceMetaLastFetchAt] = m.LastFetchAt.UTC().Format(time.RFC3339)
	}
	if m.LastHTTPStatus != 0 {
		out[SourceMetaLastHTTPStatus] = m.LastHTTPStatus
	}
	if m.FinalURL != "" {
		out[SourceMetaFinalURL] = m.FinalURL
	}
	return out
}

// SourceMetaFromMap 从 jsonb 解码后的 map 还原 SourceMeta：缺失或类型不符的键按零值处理，
// 不做报错（历史数据可能由更早版本写入或部分缺键）。
func SourceMetaFromMap(raw map[string]any) SourceMeta {
	if len(raw) == 0 {
		return SourceMeta{}
	}
	return SourceMeta{
		ETag:           stringValue(raw[SourceMetaETag]),
		LastModified:   stringValue(raw[SourceMetaLastModified]),
		LastEngine:     stringValue(raw[SourceMetaLastEngine]),
		LastFetchAt:    timeValue(raw[SourceMetaLastFetchAt]),
		LastHTTPStatus: intValue(raw[SourceMetaLastHTTPStatus]),
		FinalURL:       stringValue(raw[SourceMetaFinalURL]),
	}
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

// timeValue 解析 RFC3339 字符串；PG jsonb 经驱动解码后也可能是 time.Time，一并兼容。
func timeValue(value any) time.Time {
	switch v := value.(type) {
	case time.Time:
		return v.UTC()
	case string:
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}
		}
		return parsed.UTC()
	}
	return time.Time{}
}

// intValue 兼容 jsonb 数字的多种解码形态（float64/json.Number/int）。
func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case interface{ Float64() (float64, error) }:
		f, err := v.Float64()
		if err != nil {
			return 0
		}
		return int(f)
	}
	return 0
}
