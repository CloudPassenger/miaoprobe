package otelsetup

import (
	"context"
	"strings"
	"testing"
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
