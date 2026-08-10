// Package otel 封装 OpenTelemetry TracerProvider 与 MeterProvider 的初始化、
// exporter 配置与优雅关闭。琅嬛的可观测性统一通过 OTel SDK 表达：
//
//   - traces：按 ratio sampler 采样，可选 OTLP 推送。
//   - metrics：OTel Meter + Prometheus exporter（暴露 /metrics），可选 OTLP 推送。
//
// OTLP exporter 默认关闭；显式配置 observability.otlp.endpoint 后才推送。
package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	otelpmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/version"
)

// Providers 持有初始化完成的 TracerProvider 与 MeterProvider，以及 Prometheus reader。
type Providers struct {
	TracerProvider trace.TracerProvider
	MeterProvider  otelpmetric.MeterProvider
	// PrometheusReader 暴露给 /metrics 端点采集（OTel Prometheus exporter）。
	PrometheusReader sdkmetric.Reader
	// tracerProvider 与 meterProvider 的原始句柄，用于 Shutdown。
	tp *sdktrace.TracerProvider
	mp *sdkmetric.MeterProvider
}

// Shutdown 释放所有 provider 与 exporter 资源。
func (p *Providers) Shutdown(ctx context.Context) error {
	var errs []error
	if p.tp != nil {
		if err := p.tp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer provider shutdown: %w", err))
		}
	}
	if p.mp != nil {
		if err := p.mp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider shutdown: %w", err))
		}
	}
	return errors.Join(errs...)
}

// Setup 初始化 OTel providers。返回的 Providers 供进程使用，shutdown 时调用 Shutdown。
//
// 行为：
//   - traces.enabled=false 时返回 noop TracerProvider。
//   - metrics 永远注册 Prometheus exporter（暴露 /metrics）；otlp.enabled 时追加 OTLP exporter。
//   - otlp.enabled=true 但 endpoint 为空时返回错误。
func Setup(ctx context.Context, cfg config.ObservabilityConfig, log *slog.Logger) (*Providers, error) {
	res, err := newResource(ctx)
	if err != nil {
		return nil, err
	}

	providers := &Providers{}

	// ---- TracerProvider ----
	if cfg.Traces.Enabled {
		sampler := sdktrace.TraceIDRatioBased(cfg.Traces.SampleRate)
		var spanExporter sdktrace.SpanExporter
		if cfg.OTLP.Enabled {
			exp, err := newTraceExporter(ctx, cfg.OTLP)
			if err != nil {
				return nil, fmt.Errorf("创建 OTLP trace exporter 失败: %w", err)
			}
			spanExporter = exp
		}
		opts := []sdktrace.TracerProviderOption{
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sampler),
		}
		if spanExporter != nil {
			opts = append(opts, sdktrace.WithBatcher(spanExporter))
		}
		tp := sdktrace.NewTracerProvider(opts...)
		providers.TracerProvider = tp
		providers.tp = tp
		otel.SetTracerProvider(tp)
		if log != nil {
			log.Info("OTel traces 已启用", "sample_rate", cfg.Traces.SampleRate, "otlp", cfg.OTLP.Enabled)
		}
	} else {
		providers.TracerProvider = trace.NewNoopTracerProvider()
	}

	// ---- MeterProvider ----
	// Prometheus exporter 注册到默认 registry，/metrics 端点用默认 promhttp.Handler() 即可采集。
	promReader, err := prometheus.New(prometheus.WithRegisterer(stdprometheus.DefaultRegisterer))
	if err != nil {
		return nil, fmt.Errorf("创建 Prometheus exporter 失败: %w", err)
	}
	providers.PrometheusReader = promReader
	mpOpts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(promReader),
	}
	if cfg.OTLP.Enabled {
		metricExporter, err := newMetricExporter(ctx, cfg.OTLP)
		if err != nil {
			return nil, fmt.Errorf("创建 OTLP metric exporter 失败: %w", err)
		}
		// 60s 周期推送。
		mpOpts = append(mpOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter,
			sdkmetric.WithInterval(60*time.Second))))
		if log != nil {
			log.Info("OTel metrics OTLP 推送已启用", "endpoint", cfg.OTLP.Endpoint)
		}
	}
	mp := sdkmetric.NewMeterProvider(mpOpts...)
	providers.MeterProvider = mp
	providers.mp = mp
	otel.SetMeterProvider(mp)

	return providers, nil
}

func newResource(ctx context.Context) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("langhuan"),
			semconv.ServiceVersion(version.Version()),
		),
	)
}

func newTraceExporter(ctx context.Context, cfg config.OTLPConfig) (sdktrace.SpanExporter, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("otlp.endpoint 不能为空")
	}
	if cfg.Protocol == "http" {
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return otlptracehttp.New(ctx, opts...)
	}
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(ctx, opts...)
}

func newMetricExporter(ctx context.Context, cfg config.OTLPConfig) (sdkmetric.Exporter, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("otlp.endpoint 不能为空")
	}
	if cfg.Protocol == "http" {
		opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		return otlpmetrichttp.New(ctx, opts...)
	}
	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}
	return otlpmetricgrpc.New(ctx, opts...)
}
