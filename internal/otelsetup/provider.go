package otelsetup

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	runtimemetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
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
	// ServiceInstanceID is reported as the service.instance.id resource
	// attribute. When empty, an ID supplied by the standard OTel resource
	// environment is preserved; otherwise the host name is used.
	ServiceInstanceID string
	// ServiceVersion is reported as the standard service.version resource
	// attribute and as the version label on miaoprobe_build_info.
	ServiceVersion string
	// ServiceRevision is reported as vcs.ref.head.revision and as the
	// revision label on miaoprobe_build_info.
	ServiceRevision string
	// BuildDate is reported as miaoprobe.build.date and on build_info.
	BuildDate string
	// EmbeddedScriptsVersion identifies the scripts baked into this binary.
	// It remains distinct from scripts loaded through --scripts.
	EmbeddedScriptsVersion string

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
	// ServiceInstanceID is the effective service.instance.id sent over OTLP.
	ServiceInstanceID string
}

// New builds a MeterProvider with a Prometheus pull reader, plus an OTLP
// push reader when cfg.OTLPEndpoint is set.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("otelsetup: determine service instance ID: %w", err)
	}
	res, err := buildResource(resource.Default(), cfg, hostname)
	if err != nil {
		return nil, fmt.Errorf("otelsetup: build resource: %w", err)
	}
	instanceID, _ := res.Set().Value(semconv.ServiceInstanceIDKey)

	reg := prometheus.NewRegistry()
	if err := registerBuildInfo(reg, cfg); err != nil {
		return nil, fmt.Errorf("otelsetup: register build info: %w", err)
	}
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
		MeterProvider:     mp,
		Registry:          reg,
		ServiceInstanceID: instanceID.AsString(),
	}, nil
}

func buildResource(base *resource.Resource, cfg Config, hostname string) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{semconv.ServiceName(cfg.ServiceName)}
	if value := strings.TrimSpace(cfg.ServiceVersion); value != "" {
		attrs = append(attrs, semconv.ServiceVersion(value))
	}
	if value := strings.TrimSpace(cfg.ServiceRevision); value != "" {
		attrs = append(attrs, attribute.String("vcs.ref.head.revision", value))
	}
	if value := strings.TrimSpace(cfg.BuildDate); value != "" {
		attrs = append(attrs, attribute.String("miaoprobe.build.date", value))
	}
	if value := strings.TrimSpace(cfg.EmbeddedScriptsVersion); value != "" {
		attrs = append(attrs, attribute.String("miaoprobe.scripts.embedded.version", value))
	}
	if instanceID := strings.TrimSpace(cfg.ServiceInstanceID); instanceID != "" {
		attrs = append(attrs, semconv.ServiceInstanceID(instanceID))
	}
	res, err := resource.Merge(base, resource.NewSchemaless(attrs...))
	if err != nil {
		return nil, err
	}
	if instanceID, ok := res.Set().Value(semconv.ServiceInstanceIDKey); ok && instanceID.AsString() != "" {
		return res, nil
	}

	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return nil, errors.New("host name is empty and no service instance ID was configured")
	}
	return resource.Merge(res, resource.NewSchemaless(semconv.ServiceInstanceID(hostname)))
}

func registerBuildInfo(reg prometheus.Registerer, cfg Config) error {
	labels := prometheus.Labels{
		"version":                  buildInfoValue(cfg.ServiceVersion, "unknown"),
		"revision":                 buildInfoValue(cfg.ServiceRevision, "unknown"),
		"build_date":               buildInfoValue(cfg.BuildDate, "unknown"),
		"embedded_scripts_version": strings.TrimSpace(cfg.EmbeddedScriptsVersion),
	}
	collector := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace:   "miaoprobe",
		Name:        "build_info",
		Help:        "Build and embedded script metadata for this miaoprobe process; always 1.",
		ConstLabels: labels,
	}, func() float64 { return 1 })
	return reg.Register(collector)
}

func buildInfoValue(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
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
