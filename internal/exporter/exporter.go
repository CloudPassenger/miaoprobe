package exporter

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/CloudPassenger/miaoprobe/internal/logging"
	"github.com/CloudPassenger/miaoprobe/internal/network"
	"github.com/CloudPassenger/miaoprobe/internal/probe"
	"github.com/CloudPassenger/miaoprobe/internal/script"
)

// Metrics holds the Prometheus collectors exposed by serve mode.
type Metrics struct {
	UnlockStatus *prometheus.GaugeVec
	Duration     *prometheus.GaugeVec
	Errors       *prometheus.CounterVec
}

// NewMetrics registers the miaoprobe_* collectors on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		UnlockStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "miaoprobe_unlock_status",
			Help: "Unlock status reported by a script: 1=unlocked 0=failed 0.5=warning -1=unknown",
		}, []string{"id", "name", "region", "tags"}),
		Duration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "miaoprobe_check_duration_seconds",
			Help: "Duration of the last execution of a script",
		}, []string{"id"}),
		Errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "miaoprobe_check_errors_total",
			Help: "Number of script executions that errored (script fault or timeout, not a business failure result)",
		}, []string{"id"}),
	}
	reg.MustRegister(m.UnlockStatus, m.Duration, m.Errors)
	return m
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
	p.pollOnce()

	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollOnce()
		}
	}
}

func (p *Poller) pollOnce() {
	logger := p.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	for _, sc := range p.Scripts {
		outcome := probe.Run(sc, p.Proxy, p.Timeout, logger)
		p.Metrics.Duration.WithLabelValues(sc.ID).Set(outcome.Duration.Seconds())

		if outcome.Err != nil {
			p.Metrics.Errors.WithLabelValues(sc.ID).Inc()
			logger.Error("script execution error", "script", sc.ID, "err", outcome.Err)
			continue
		}

		cs := ClassifyColor(outcome.Result.Background)
		if cs.Skip {
			continue
		}
		if !cs.Recognized {
			logger.Warn("unrecognized background color, exporting status -1", "script", sc.ID, "background", outcome.Result.Background)
		}

		p.Metrics.UnlockStatus.WithLabelValues(sc.ID, sc.Name, strings.Join(sc.Regions, ","), strings.Join(sc.Tags, ",")).Set(cs.Value)
	}
}
