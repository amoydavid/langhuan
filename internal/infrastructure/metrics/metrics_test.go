package metrics

import (
	"context"
	"strings"
	"testing"
	"time"

	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

// TestMetricNamesMatchPromQLContract 验证 OTel exporter 输出的 Prometheus 指标名
// 与设计契约一致（langhuan_http_requests_total 等）。
// 回归保护：counter 若误加 WithUnit("1")，exporter 会输出 *_ratio_total，本测试将失败。
func TestMetricNamesMatchPromQLContract(t *testing.T) {
	reg := stdprometheus.NewRegistry()
	exporter, err := prometheus.New(prometheus.WithRegisterer(reg))
	if err != nil {
		t.Fatal(err)
	}
	mp := metric.NewMeterProvider(metric.WithReader(exporter))
	defer func() { _ = mp.Shutdown(context.Background()) }()

	m := New(mp)
	// 触发各指标产生数据。
	m.ObserveHTTPRequest("GET", "/healthz", "200", 10*time.Millisecond)
	m.ObserveStage(context.Background(), "parse", "ok", 5*time.Millisecond)

	// 从 registry Gather 出指标名。
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool, len(metricFamilies))
	for _, mf := range metricFamilies {
		names[mf.GetName()] = true
	}

	// 设计契约要求的指标名（legacy 下划线转义形式）。
	for _, want := range []string{
		"langhuan_http_requests_total",
		"langhuan_http_request_duration_seconds",
		"langhuan_document_stage_total",
		"langhuan_document_stage_duration_seconds",
	} {
		if !names[want] {
			t.Fatalf("指标 %q 未产出；实际指标名：%v", want, names)
		}
	}
	// 回归：绝不出现 _ratio 后缀（unit=1 误用）。
	for name := range names {
		if strings.Contains(name, "_ratio_total") {
			t.Fatalf("指标名 %q 含 _ratio_total（counter 误设 WithUnit(1)）", name)
		}
	}
}
