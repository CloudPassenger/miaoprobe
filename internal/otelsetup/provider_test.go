package otelsetup

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func TestWithDefaultMetricsPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://otlp-gateway-prod-us-central-0.grafana.net/otlp", "https://otlp-gateway-prod-us-central-0.grafana.net/otlp/v1/metrics"},
		{"http://127.0.0.1:4318", "http://127.0.0.1:4318/v1/metrics"},
		{"https://example.com/v1/metrics", "https://example.com/v1/metrics"},
		{"https://example.com/otlp/", "https://example.com/otlp/v1/metrics"},
	}
	for _, c := range cases {
		if got := withDefaultMetricsPath(c.in); got != c.want {
			t.Errorf("withDefaultMetricsPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseHeaders(t *testing.T) {
	got, err := ParseHeaders("Authorization=Basic xxxx, X-Foo=bar")
	if err != nil {
		t.Fatalf("ParseHeaders: %v", err)
	}
	if got["Authorization"] != "Basic xxxx" || got["X-Foo"] != "bar" {
		t.Fatalf("unexpected headers: %+v", got)
	}

	if got, err := ParseHeaders(""); err != nil || got != nil {
		t.Fatalf("ParseHeaders(\"\") = %+v, %v, want nil, nil", got, err)
	}

	if _, err := ParseHeaders("no-equals-sign"); err == nil {
		t.Fatal("expected error for malformed header")
	}
}

func TestBuildResourceServiceInstanceID(t *testing.T) {
	cases := []struct {
		name       string
		base       *resource.Resource
		configured string
		hostname   string
		want       string
	}{
		{
			name:       "configured ID overrides environment resource",
			base:       resource.NewSchemaless(semconv.ServiceInstanceID("from-env")),
			configured: "edge-hk-01",
			hostname:   "host-a",
			want:       "edge-hk-01",
		},
		{
			name:     "environment resource is preserved",
			base:     resource.NewSchemaless(semconv.ServiceInstanceID("from-env")),
			hostname: "host-a",
			want:     "from-env",
		},
		{
			name:     "host name is the default",
			base:     resource.Empty(),
			hostname: "host-a",
			want:     "host-a",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := buildResource(tc.base, Config{
				ServiceName:       "miaoprobe",
				ServiceInstanceID: tc.configured,
			}, tc.hostname)
			if err != nil {
				t.Fatalf("buildResource: %v", err)
			}
			got, ok := res.Set().Value(semconv.ServiceInstanceIDKey)
			if !ok || got.AsString() != tc.want {
				t.Fatalf("service.instance.id = %q, %v, want %q, true", got.AsString(), ok, tc.want)
			}
		})
	}
}

func gatherNames(t *testing.T, p *Provider) map[string]bool {
	t.Helper()
	mfs, err := p.Registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	names := make(map[string]bool, len(mfs))
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}
	return names
}

func TestRuntimeMetricsOptIn(t *testing.T) {
	off, err := New(context.Background(), Config{ServiceName: "test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer off.Shutdown(context.Background())

	for name := range gatherNames(t, off) {
		if strings.HasPrefix(name, "go_") || strings.HasPrefix(name, "process_") {
			t.Errorf("runtime metric %q exported with RuntimeMetrics disabled", name)
		}
	}

	on, err := New(context.Background(), Config{ServiceName: "test", RuntimeMetrics: true})
	if err != nil {
		t.Fatalf("New with runtime metrics: %v", err)
	}
	defer on.Shutdown(context.Background())

	names := gatherNames(t, on)
	// From OTel's runtime instrumentation: these also reach an OTLP reader.
	for _, want := range []string{"go_goroutine_count", "go_memory_used_bytes"} {
		if !names[want] {
			t.Errorf("expected OTel runtime metric %q", want)
		}
	}
	// From the Prometheus process collector: /metrics only, but the only
	// source of the fd and RSS counters that reveal leaks.
	for _, want := range []string{"process_open_fds", "process_resident_memory_bytes"} {
		if !names[want] {
			t.Errorf("expected process metric %q", want)
		}
	}
}

func TestNewWithoutOTLPServesPrometheus(t *testing.T) {
	p, err := New(context.Background(), Config{ServiceName: "test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Shutdown(context.Background())

	meter := p.MeterProvider.Meter("test")
	counter, err := meter.Int64Counter("miaoprobe_check_errors")
	if err != nil {
		t.Fatalf("Int64Counter: %v", err)
	}
	counter.Add(context.Background(), 1)

	mfs, err := p.Registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var found bool
	for _, mf := range mfs {
		if mf.GetName() == "miaoprobe_check_errors_total" {
			found = true
		}
	}
	if !found {
		names := make([]string, len(mfs))
		for i, mf := range mfs {
			names[i] = mf.GetName()
		}
		t.Fatalf("miaoprobe_check_errors_total not found among %s", strings.Join(names, ", "))
	}
}
