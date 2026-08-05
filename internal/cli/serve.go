package cli

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/CloudPassenger/miaoprobe/internal/exporter"
	"github.com/CloudPassenger/miaoprobe/internal/network"
	"github.com/CloudPassenger/miaoprobe/internal/otelsetup"
	"github.com/CloudPassenger/miaoprobe/internal/script"
)

// serveOpts holds the flags for the serve subcommand.
type serveOpts struct {
	proxyRaw, listen  string
	interval, timeout time.Duration
	concurrency       int
	scripts           []script.Script
	filterSpec        script.FilterSpec

	runtimeMetrics bool

	otelEndpoint, otelProtocol, otelHeadersRaw string
	otelInsecure                               bool
	otelInterval                               time.Duration
}

func newServeCommand() *cobra.Command {
	var o serveOpts
	var scriptsPath, filterRaw string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Poll scripts on an interval and expose results as Prometheus metrics / push them via OTLP",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, err := loggerFromFlags(cmd)
			if err != nil {
				return err
			}
			filterSpec, err := resolveFilterSpec(cmd, true)
			if err != nil {
				return err
			}
			scripts, err := resolveScripts(cmd, scriptsPath, logger)
			if err != nil {
				return err
			}
			o.scripts = scripts
			o.filterSpec = filterSpec
			return runServe(o, logger)
		},
	}

	cmd.Flags().StringVar(&scriptsPath, "scripts", "", "directory containing index.json (defaults to this build's embedded miaospeed-scripts, if any)")
	cmd.Flags().StringVar(&o.proxyRaw, "probe.proxy", "", "egress proxy: http://host:port or socks5://host:port (empty = direct)")
	cmd.Flags().StringVar(&filterRaw, "filter", "", `script selection, e.g. "category:media,ai;region:hk,us;id:netflix;mode:exclude" (see "miaoprobe list" and README.md#configuration)`)
	cmd.Flags().DurationVar(&o.interval, "probe.interval", 5*time.Minute, "polling interval")
	cmd.Flags().DurationVar(&o.timeout, "probe.timeout", 30*time.Second, "per-script execution timeout")
	cmd.Flags().IntVar(&o.concurrency, "probe.concurrency", exporter.DefaultConcurrency, "how many scripts to probe in parallel")
	cmd.Flags().StringVar(&o.listen, "metrics.listen", ":9765", "address to expose /metrics on")
	cmd.Flags().BoolVar(&o.runtimeMetrics, "metrics.runtime", true, "also export Go runtime and process metrics (goroutines, heap, GC, open fds, resident memory) for diagnosing miaoprobe itself")

	cmd.Flags().StringVar(&o.otelEndpoint, "otel.endpoint", "", "OTLP endpoint to push metrics to (e.g. Grafana Cloud's OTLP gateway); empty disables push, honors OTEL_EXPORTER_OTLP_* env vars")
	cmd.Flags().StringVar(&o.otelProtocol, "otel.protocol", "http/protobuf", "OTLP wire protocol: http/protobuf or grpc")
	cmd.Flags().StringVar(&o.otelHeadersRaw, "otel.headers", "", "comma-separated key=value headers sent with every OTLP export (e.g. Authorization=Basic <base64>)")
	cmd.Flags().BoolVar(&o.otelInsecure, "otel.insecure", false, "disable TLS for the OTLP connection (local collectors only)")
	cmd.Flags().DurationVar(&o.otelInterval, "otel.interval", time.Minute, "how often buffered metrics are pushed to --otel.endpoint")

	return cmd
}

func runServe(o serveOpts, logger *slog.Logger) error {
	proxyCfg, err := network.ParseProxy(o.proxyRaw)
	if err != nil {
		return err
	}
	otelHeaders, err := otelsetup.ParseHeaders(o.otelHeadersRaw)
	if err != nil {
		return err
	}

	scripts := script.Select(o.scripts, o.filterSpec)
	logger.Info("loaded scripts", "count", len(scripts))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	provider, err := otelsetup.New(ctx, otelsetup.Config{
		ServiceName:  "miaoprobe",
		OTLPEndpoint: o.otelEndpoint,
		OTLPProtocol: o.otelProtocol,
		OTLPHeaders:  otelHeaders,
		OTLPInsecure: o.otelInsecure,
		OTLPInterval: o.otelInterval,

		RuntimeMetrics: o.runtimeMetrics,
	})
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := provider.Shutdown(shutdownCtx); err != nil {
			logger.Error("otel shutdown failed", "err", err)
		}
	}()

	meter := provider.MeterProvider.Meter("github.com/CloudPassenger/miaoprobe")
	metrics, err := exporter.NewMetrics(meter)
	if err != nil {
		return err
	}

	poller := &exporter.Poller{
		Scripts:     scripts,
		Proxy:       proxyCfg,
		Timeout:     o.timeout,
		Interval:    o.interval,
		Metrics:     metrics,
		Logger:      logger,
		Concurrency: o.concurrency,
	}

	// Wait for the poller to finish its in-flight cycle before Shutdown
	// flushes, so the final OTLP export includes the last recorded values.
	pollerDone := make(chan struct{})
	go func() {
		defer close(pollerDone)
		poller.Run(ctx)
	}()
	// Registered after the provider.Shutdown defer above, so it runs first
	// (defers are LIFO).
	defer func() {
		select {
		case <-pollerDone:
		case <-time.After(5 * time.Second):
			logger.Warn("poller did not stop in time; exporting anyway")
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(provider.Registry, promhttp.HandlerOpts{}))

	// Timeouts are mandatory for a long-lived listener: without them a
	// client that opens a connection and never finishes its request holds a
	// file descriptor forever, so a handful of half-open connections can
	// exhaust the process's fd limit.
	srv := &http.Server{
		Addr:              o.listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if o.otelEndpoint != "" {
		logger.Info("pushing metrics via otlp", "endpoint", o.otelEndpoint, "protocol", o.otelProtocol, "interval", o.otelInterval)
	}
	logger.Info("serving metrics", "listen", o.listen, "interval", o.interval, "concurrency", poller.Concurrency)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
