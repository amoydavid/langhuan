// Package metrics 用 OpenTelemetry Meter 表达琅嬛的指标。
//
// 底层完全基于 OTel SDK：指标经 MeterProvider 产出，由 otel 包统一注册的
// Prometheus exporter（暴露 /metrics）和可选 OTLP exporter 导出。
//
// 指标分三类：
//   - HTTP 请求（中间件采集）
//   - 导入主链路各阶段（parse/chunk/index）
//   - RAG 检索（retrieval 总量/耗时、rerank 总量）
package metrics

import (
	"context"
	"time"

	stdprommodel "github.com/prometheus/common/model"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics 持有所有 OTel 指标句柄。零值不可用，必须用 New 构造。
type Metrics struct {
	meter                 metric.Meter
	httpRequestsTotal     metric.Int64Counter
	httpRequestDuration   metric.Float64Histogram
	documentStageTotal    metric.Int64Counter
	documentStageDuration metric.Float64Histogram
}

// New 用给定 MeterProvider 构造指标集。mp 为 nil 时回退到全局 MeterProvider。
func New(mp metric.MeterProvider) *Metrics {
	// 固定 legacy 下划线指标名（langhuan_http_requests_total），与 PromQL/告警契约一致；
	// prometheus/common 默认 UTF8Validation 会保留点号（langhuan.http.requests_total），破坏契约。
	stdprommodel.NameValidationScheme = stdprommodel.LegacyValidation
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	meter := mp.Meter("langhuan")
	m := &Metrics{meter: meter}
	m.httpRequestsTotal = counter(meter, "langhuan.http.requests.total", "HTTP 请求总数")
	m.httpRequestDuration = histogram(meter, "langhuan.http.request.duration.seconds", "HTTP 请求耗时（秒）")
	m.documentStageTotal = counter(meter, "langhuan.document.stage.total", "导入主链路各阶段执行次数")
	m.documentStageDuration = histogram(meter, "langhuan.document.stage.duration.seconds", "导入主链路各阶段耗时（秒）")
	return m
}

func counter(meter metric.Meter, name, desc string) metric.Int64Counter {
	// 注意：不设置 WithUnit("1")——OTel Prometheus exporter 会把 unit "1" 转成
	// `_ratio` 后缀（如 langhuan_http_requests_ratio_total），破坏与设计契约的指标名。
	c, err := meter.Int64Counter(name, metric.WithDescription(desc))
	if err != nil {
		return nil // 已注册或失败；调用方 nil-check 容忍（测试重复构造场景）。
	}
	return c
}

func histogram(meter metric.Meter, name, desc string) metric.Float64Histogram {
	h, err := meter.Float64Histogram(name, metric.WithDescription(desc), metric.WithUnit("s"))
	if err != nil {
		return nil
	}
	return h
}

// ObserveHTTPRequest 记录一次 HTTP 请求的计数与耗时。
func (m *Metrics) ObserveHTTPRequest(method, route, status string, duration time.Duration) {
	if m == nil || m.httpRequestsTotal == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("method", method),
		attribute.String("route", route),
		attribute.String("status", status),
	}
	m.httpRequestsTotal.Add(context.Background(), 1, metric.WithAttributes(attrs...))
	if m.httpRequestDuration != nil {
		m.httpRequestDuration.Record(context.Background(), duration.Seconds(), metric.WithAttributes(attrs...))
	}
}

// ObserveStage 记录导入链路一个阶段的计数与耗时，实现 worker.StageRecorder。
func (m *Metrics) ObserveStage(_ context.Context, stage, status string, duration time.Duration) {
	if m == nil || m.documentStageTotal == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("stage", stage),
		attribute.String("status", status),
	}
	m.documentStageTotal.Add(context.Background(), 1, metric.WithAttributes(attrs...))
	if m.documentStageDuration != nil {
		m.documentStageDuration.Record(context.Background(), duration.Seconds(), metric.WithAttributes(attrs...))
	}
}

// StageRecorder 是导入阶段耗时的抽象接口，供 worker 驱动层注入。
type StageRecorder interface {
	ObserveStage(ctx context.Context, stage, status string, duration time.Duration)
}

// Ensure Metrics 实现 StageRecorder。
var _ StageRecorder = (*Metrics)(nil)
