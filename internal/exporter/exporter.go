package exporter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/CloudPassenger/miaoprobe/internal/logging"
	"github.com/CloudPassenger/miaoprobe/internal/network"
	"github.com/CloudPassenger/miaoprobe/internal/probe"
	"github.com/CloudPassenger/miaoprobe/internal/script"
)

// StatusUnknown is the value reported for a script whose real status could
// not be determined this cycle (execution error, or an unrecognized
// status/background). It is written explicitly rather than skipped: gauges
// are cumulative last-value aggregations, so leaving a series untouched
// makes /metrics keep serving the previous — possibly hours old — value as
// if it were current.
const StatusUnknown = -1

// Metrics holds the OpenTelemetry instruments exposed by serve mode. They
// are recorded once per poll and read by every attached MeterProvider
// reader (Prometheus pull, OTLP push, ...).
type Metrics struct {
	UnlockStatus otelmetric.Float64Gauge
	Duration     otelmetric.Float64Gauge
	Errors       otelmetric.Int64Counter
	LastSuccess  otelmetric.Float64Gauge
	ScriptInfo   otelmetric.Int64Gauge
	PollDuration otelmetric.Float64Gauge
}

// NewMetrics creates the miaoprobe_* instruments on meter. Note that
// counters get a "_total" suffix from the Prometheus bridge automatically,
// so Errors is named without one here.
func NewMetrics(meter otelmetric.Meter) (*Metrics, error) {
	unlockStatus, err1 := meter.Float64Gauge("miaoprobe_unlock_status",
		otelmetric.WithDescription("Unlock status reported by a script: 1=unlocked 0=failed 0.5=warning -1=unknown/error"))
	duration, err2 := meter.Float64Gauge("miaoprobe_check_duration_seconds",
		otelmetric.WithDescription("Duration of the last execution of a script"))
	checkErrors, err3 := meter.Int64Counter("miaoprobe_check_errors",
		otelmetric.WithDescription("Number of script executions that errored (script fault or timeout, not a business failure result)"))
	lastSuccess, err4 := meter.Float64Gauge("miaoprobe_last_success_timestamp_seconds",
		otelmetric.WithDescription("Unix timestamp of the last execution that produced a usable status; alert on staleness with time() - this"))
	scriptInfo, err5 := meter.Int64Gauge("miaoprobe_script_info",
		otelmetric.WithDescription("Static script metadata as labels; always 1. Join against miaoprobe_unlock_status on the id label"))
	pollDuration, err6 := meter.Float64Gauge("miaoprobe_poll_duration_seconds",
		otelmetric.WithDescription("Wall-clock duration of the last full polling cycle across all scripts"))
	if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
		return nil, fmt.Errorf("exporter: create instruments: %w", err)
	}
	return &Metrics{
		UnlockStatus: unlockStatus,
		Duration:     duration,
		Errors:       checkErrors,
		LastSuccess:  lastSuccess,
		ScriptInfo:   scriptInfo,
		PollDuration: pollDuration,
	}, nil
}

// DefaultConcurrency is the number of scripts polled in parallel when
// Poller.Concurrency is unset.
const DefaultConcurrency = 8

// Poller periodically runs a set of scripts and updates Metrics.
type Poller struct {
	Scripts  []script.Script
	Proxy    *network.ProxyConfig
	Timeout  time.Duration
	Interval time.Duration
	Metrics  *Metrics
	Logger   *slog.Logger

	// Concurrency bounds how many scripts are probed in parallel.
	// Defaults to DefaultConcurrency; values below 1 are treated as the
	// default. Each worker builds its own goja runtime, so this also caps
	// peak memory and file-descriptor use.
	Concurrency int

	runner *probe.Runner
}

// Run executes one polling cycle immediately, then every Interval, until ctx
// is canceled.
func (p *Poller) Run(ctx context.Context) {
	logger := p.logger()

	// Compile every script once up front instead of on each poll.
	p.runner = probe.NewRunner(p.Scripts, logger)
	p.publishScriptInfo(ctx)

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

func (p *Poller) logger() *slog.Logger {
	if p.Logger == nil {
		return logging.Discard()
	}
	return p.Logger
}

func (p *Poller) concurrency() int {
	n := p.Concurrency
	if n < 1 {
		n = DefaultConcurrency
	}
	if n > len(p.Scripts) {
		n = len(p.Scripts)
	}
	if n < 1 {
		n = 1
	}
	return n
}

// publishScriptInfo emits the static metadata series once per process.
// Keeping name/region/tags here rather than on miaoprobe_unlock_status means
// editing a script's metadata cannot orphan a status series: gauge attribute
// sets are never forgotten, so a label change would otherwise leave the old
// series behind forever and double-count in aggregations.
func (p *Poller) publishScriptInfo(ctx context.Context) {
	for _, sc := range p.Scripts {
		p.Metrics.ScriptInfo.Record(ctx, 1, otelmetric.WithAttributes(
			attribute.String("id", sc.ID),
			attribute.String("name", sc.Name),
			attribute.String("category", sc.Category),
			attribute.String("region", strings.Join(sc.Regions, ",")),
			attribute.String("tags", strings.Join(sc.Tags, ",")),
		))
	}
}

func (p *Poller) pollOnce(ctx context.Context) {
	logger := p.logger()
	start := time.Now()

	workers := p.concurrency()
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, sc := range p.Scripts {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(sc script.Script) {
			defer wg.Done()
			defer func() { <-sem }()
			// A panic inside a script's host calls would otherwise take the
			// whole process down and end the polling loop for good.
			defer func() {
				if r := recover(); r != nil {
					logger.Error("recovered panic while probing script", "script", sc.ID,
						"panic", r, "stack", string(debugStack()))
					p.recordUnknown(ctx, sc)
				}
			}()
			p.probeAndRecord(ctx, sc, logger)
		}(sc)
	}

	wg.Wait()

	elapsed := time.Since(start)
	p.Metrics.PollDuration.Record(ctx, elapsed.Seconds())

	// A cycle that outlives its interval means Ticker will drop ticks and
	// the effective polling rate silently degrades, so make it visible.
	if p.Interval > 0 && elapsed > p.Interval {
		logger.Warn("poll cycle exceeded interval; consider raising --probe.concurrency or --probe.interval",
			"elapsed", elapsed, "interval", p.Interval, "scripts", len(p.Scripts), "concurrency", workers)
	} else {
		logger.Debug("poll cycle finished", "elapsed", elapsed, "scripts", len(p.Scripts), "concurrency", workers)
	}
}

func (p *Poller) probeAndRecord(ctx context.Context, sc script.Script, logger *slog.Logger) {
	outcome := p.runner.Run(ctx, sc, p.Proxy, p.Timeout, logger)
	idAttr := otelmetric.WithAttributes(attribute.String("id", sc.ID))
	p.Metrics.Duration.Record(ctx, outcome.Duration.Seconds(), idAttr)

	if outcome.Err != nil {
		p.Metrics.Errors.Add(ctx, 1, idAttr)
		// Report unknown rather than leaving the last good value in place:
		// a script that starts failing must not keep looking "unlocked".
		p.Metrics.UnlockStatus.Record(ctx, StatusUnknown, idAttr)
		logger.Error("script execution error", "script", sc.ID, "err", outcome.Err)
		return
	}

	cs := Classify(outcome.Result.Status, outcome.Result.Background)
	if !cs.Recognized && !cs.Skip {
		logger.Warn("unrecognized status/background, exporting status -1", "script", sc.ID,
			"status", outcome.Result.Status, "background", outcome.Result.Background)
	}

	// A script reporting n/a has run successfully but has no meaningful
	// status. Record unknown so the series stays fresh, and still stamp
	// LastSuccess since the check itself worked.
	value := cs.Value
	if cs.Skip {
		value = StatusUnknown
	}

	p.Metrics.UnlockStatus.Record(ctx, value, idAttr)
	p.Metrics.LastSuccess.Record(ctx, float64(time.Now().Unix()), idAttr)
}

// recordUnknown marks a script's status as indeterminate, used when probing
// it panicked and no outcome is available.
func (p *Poller) recordUnknown(ctx context.Context, sc script.Script) {
	idAttr := otelmetric.WithAttributes(attribute.String("id", sc.ID))
	p.Metrics.Errors.Add(ctx, 1, idAttr)
	p.Metrics.UnlockStatus.Record(ctx, StatusUnknown, idAttr)
}

func debugStack() []byte {
	buf := make([]byte, 8192)
	return buf[:runtime.Stack(buf, false)]
}
