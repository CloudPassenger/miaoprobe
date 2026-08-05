package exporter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/CloudPassenger/miaoprobe/internal/logging"
	"github.com/CloudPassenger/miaoprobe/internal/network"
	"github.com/CloudPassenger/miaoprobe/internal/probe"
	"github.com/CloudPassenger/miaoprobe/internal/script"
)

// Metrics holds the OpenTelemetry instruments exposed by serve mode. They
// are recorded once per poll and read by every attached MeterProvider
// reader (Prometheus pull, OTLP push, ...).
type Metrics struct {
	UnlockStatus otelmetric.Float64Gauge
	Duration     otelmetric.Float64Gauge
	Errors       otelmetric.Int64Counter
}

// NewMetrics creates the miaoprobe_* instruments on meter. Note that
// counters get a "_total" suffix from the Prometheus bridge automatically,
// so Errors is named without one here.
func NewMetrics(meter otelmetric.Meter) (*Metrics, error) {
	unlockStatus, err1 := meter.Float64Gauge("miaoprobe_unlock_status",
		otelmetric.WithDescription("Unlock status reported by a script: 1=unlocked 0=failed 0.5=warning -1=unknown"))
	duration, err2 := meter.Float64Gauge("miaoprobe_check_duration_seconds",
		otelmetric.WithDescription("Duration of the last execution of a script"))
	checkErrors, err3 := meter.Int64Counter("miaoprobe_check_errors",
		otelmetric.WithDescription("Number of script executions that errored (script fault or timeout, not a business failure result)"))
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, fmt.Errorf("exporter: create instruments: %w", err)
	}
	return &Metrics{UnlockStatus: unlockStatus, Duration: duration, Errors: checkErrors}, nil
}

// Poller periodically runs a set of scripts and updates Metrics.
type Poller struct {
	Scripts  []script.Script
	Proxy    *network.ProxyConfig
	Timeout  time.Duration
	Interval time.Duration
	Metrics  *Metrics
	Logger   *slog.Logger
}

// Run executes one polling cycle immediately, then every Interval, until ctx
// is canceled.
func (p *Poller) Run(ctx context.Context) {
	p.pollOnce(ctx)

	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context) {
	logger := p.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	for _, sc := range p.Scripts {
		outcome := probe.Run(ctx, sc, p.Proxy, p.Timeout, logger)
		idAttr := otelmetric.WithAttributes(attribute.String("id", sc.ID))
		p.Metrics.Duration.Record(ctx, outcome.Duration.Seconds(), idAttr)

		if outcome.Err != nil {
			p.Metrics.Errors.Add(ctx, 1, idAttr)
			logger.Error("script execution error", "script", sc.ID, "err", outcome.Err)
			continue
		}

		cs := Classify(outcome.Result.Status, outcome.Result.Background)
		if cs.Skip {
			continue
		}
		if !cs.Recognized {
			logger.Warn("unrecognized status/background, exporting status -1", "script", sc.ID, "status", outcome.Result.Status, "background", outcome.Result.Background)
		}

		p.Metrics.UnlockStatus.Record(ctx, cs.Value, otelmetric.WithAttributes(
			attribute.String("id", sc.ID),
			attribute.String("name", sc.Name),
			attribute.String("region", strings.Join(sc.Regions, ",")),
			attribute.String("tags", strings.Join(sc.Tags, ",")),
		))
	}
}
