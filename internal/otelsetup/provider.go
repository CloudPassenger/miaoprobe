package otelsetup

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	runtimemetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	otlpmetricgrpc "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config controls how the MeterProvider is assembled.
type Config struct {
	// ServiceName is reported as the service.name resource attribute.
	ServiceName string

	// OTLPEndpoint is the remote OpenTelemetry endpoint to push metrics to,
	// e.g. "https://otlp-gateway-prod-xx.grafana.net/otlp" (http/protobuf)
	// or "otel-collector:4317" (grpc). Push is disabled when empty.
	OTLPEndpoint string
	// OTLPProtocol is "http/protobuf" (default) or "grpc".
	OTLPProtocol string
	// OTLPHeaders are sent with every export request, e.g. for bearer/basic
	// auth against a vendor's OTLP gateway.
	OTLPHeaders map[string]string
	// OTLPInsecure disables TLS; only useful against a local collector.
	OTLPInsecure bool
	// OTLPInterval controls how often buffered metrics are pushed.
	OTLPInterval time.Duration

	// RuntimeMetrics exports Go runtime and process telemetry alongside the
	// miaoprobe_* instruments: goroutine count, heap use, GC activity, and
	// (on /metrics only) open file descriptors, resident memory and CPU
	// time. Intended for diagnosing the exporter itself over a long
	// deployment rather than for monitoring unlock status.
	RuntimeMetrics bool
}

// Provider bundles the MeterProvider with the Prometheus registry backing
// its pull-based reader, so callers can serve /metrics from it directly.
type Provider struct {
	MeterProvider *metric.MeterProvider
	Registry      *prometheus.Registry
}

// New builds a MeterProvider with a Prometheus pull reader, plus an OTLP
// push reader when cfg.OTLPEndpoint is set.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName),
	))
	if err != nil {
		return nil, fmt.Errorf("otelsetup: build resource: %w", err)
	}

	reg := prometheus.NewRegistry()
	promExporter, err := otelprom.New(
		otelprom.WithRegisterer(reg),
		otelprom.WithoutTargetInfo(),
		otelprom.WithoutScopeInfo(),
	)
	if err != nil {
		return nil, fmt.Errorf("otelsetup: build prometheus reader: %w", err)
	}

	opts := []metric.Option{
		metric.WithResource(res),
		metric.WithReader(promExporter),
	}

	if cfg.OTLPEndpoint != "" {
		reader, err := newOTLPReader(ctx, cfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, metric.WithReader(reader))
	}

	mp := metric.NewMeterProvider(opts...)

	if cfg.RuntimeMetrics {
		// Two complementary sources, because neither covers both transports:
		//
		//   - OTel's runtime instrumentation creates instruments on the
		//     MeterProvider, so goroutine/heap/GC metrics reach every reader,
		//     including the OTLP push.
		//   - Prometheus' process collector reads /proc and registers on the
		//     Registry only, so it is /metrics-only, but it is the only
		//     source of open-fd, resident-memory and CPU-time counters --
		//     precisely the signals that reveal descriptor or memory leaks.
		if err := runtimemetrics.Start(runtimemetrics.WithMeterProvider(mp)); err != nil {
			return nil, fmt.Errorf("otelsetup: start runtime metrics: %w", err)
		}
		if err := reg.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
			return nil, fmt.Errorf("otelsetup: register process collector: %w", err)
		}
	}

	return &Provider{
		MeterProvider: mp,
		Registry:      reg,
	}, nil
}

// Shutdown flushes any buffered metrics and releases resources, including
// pushing a final export to the OTLP endpoint if push is enabled.
func (p *Provider) Shutdown(ctx context.Context) error {
	return p.MeterProvider.Shutdown(ctx)
}

func newOTLPReader(ctx context.Context, cfg Config) (metric.Reader, error) {
	var exp metric.Exporter
	var err error

	switch cfg.OTLPProtocol {
	case "", "http/protobuf":
		httpOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(withDefaultMetricsPath(cfg.OTLPEndpoint))}
		if len(cfg.OTLPHeaders) > 0 {
			httpOpts = append(httpOpts, otlpmetrichttp.WithHeaders(cfg.OTLPHeaders))
		}
		if cfg.OTLPInsecure {
			httpOpts = append(httpOpts, otlpmetrichttp.WithInsecure())
		}
		exp, err = otlpmetrichttp.New(ctx, httpOpts...)
	case "grpc":
		grpcOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpointURL(cfg.OTLPEndpoint)}
		if len(cfg.OTLPHeaders) > 0 {
			grpcOpts = append(grpcOpts, otlpmetricgrpc.WithHeaders(cfg.OTLPHeaders))
		}
		if cfg.OTLPInsecure {
			grpcOpts = append(grpcOpts, otlpmetricgrpc.WithInsecure())
		}
		exp, err = otlpmetricgrpc.New(ctx, grpcOpts...)
	default:
		return nil, fmt.Errorf("otelsetup: unknown otel protocol %q (want http/protobuf or grpc)", cfg.OTLPProtocol)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("otelsetup: build otlp exporter"), err)
	}

	interval := cfg.OTLPInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return metric.NewPeriodicReader(exp, metric.WithInterval(interval)), nil
}

// withDefaultMetricsPath appends the OTLP default metrics path
// (/v1/metrics) to endpoint if it has no path of its own, mirroring the
// behavior of the generic OTEL_EXPORTER_OTLP_ENDPOINT env var. This lets
// users pass a base URL such as Grafana Cloud's OTLP gateway
// ("https://otlp-gateway-prod-xx.grafana.net/otlp") without remembering to
// add the signal-specific suffix themselves.
func withDefaultMetricsPath(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	if strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/v1/metrics") {
		return endpoint
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/metrics"
	return u.String()
}
