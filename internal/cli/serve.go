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
	scriptsPath, proxyRaw, filterRaw, listen string
	interval, timeout                        time.Duration

	otelEndpoint, otelProtocol, otelHeadersRaw string
	otelInsecure                               bool
	otelInterval                               time.Duration
}

func newServeCommand() *cobra.Command {
	var o serveOpts

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Poll scripts on an interval and expose results as Prometheus metrics / push them via OTLP",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, err := loggerFromFlags(cmd)
			if err != nil {
				return err
			}
			return runServe(o, logger)
		},
	}

	cmd.Flags().StringVar(&o.scriptsPath, "scripts", "", "directory containing index.json (required)")
	cmd.Flags().StringVar(&o.proxyRaw, "proxy", "", "egress proxy: http://host:port or socks5://host:port (empty = direct)")
	cmd.Flags().StringVar(&o.filterRaw, "filter", "", "comma-separated region/tag filter")
	cmd.Flags().DurationVar(&o.interval, "interval", 5*time.Minute, "polling interval")
	cmd.Flags().DurationVar(&o.timeout, "timeout", 30*time.Second, "per-script execution timeout")
	cmd.Flags().StringVar(&o.listen, "listen", ":9765", "address to expose /metrics on")

	cmd.Flags().StringVar(&o.otelEndpoint, "otel-endpoint", "", "OTLP endpoint to push metrics to (e.g. Grafana Cloud's OTLP gateway); empty disables push, honors OTEL_EXPORTER_OTLP_* env vars")
	cmd.Flags().StringVar(&o.otelProtocol, "otel-protocol", "http/protobuf", "OTLP wire protocol: http/protobuf or grpc")
	cmd.Flags().StringVar(&o.otelHeadersRaw, "otel-headers", "", "comma-separated key=value headers sent with every OTLP export (e.g. Authorization=Basic <base64>)")
	cmd.Flags().BoolVar(&o.otelInsecure, "otel-insecure", false, "disable TLS for the OTLP connection (local collectors only)")
	cmd.Flags().DurationVar(&o.otelInterval, "otel-interval", time.Minute, "how often buffered metrics are pushed to --otel-endpoint")
	_ = cmd.MarkFlagRequired("scripts")

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

	scripts, err := script.Load(o.scriptsPath)
	if err != nil {
		return err
	}
	scripts = script.Filter(scripts, script.ParseFilter(o.filterRaw))
	logger.Info("loaded scripts", "count", len(scripts), "path", o.scriptsPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	provider, err := otelsetup.New(ctx, otelsetup.Config{
		ServiceName:  "miaoprobe",
		OTLPEndpoint: o.otelEndpoint,
		OTLPProtocol: o.otelProtocol,
		OTLPHeaders:  otelHeaders,
		OTLPInsecure: o.otelInsecure,
		OTLPInterval: o.otelInterval,
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
		Scripts:  scripts,
		Proxy:    proxyCfg,
		Timeout:  o.timeout,
		Interval: o.interval,
		Metrics:  metrics,
		Logger:   logger,
	}

	go poller.Run(ctx)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(provider.Registry, promhttp.HandlerOpts{}))

	srv := &http.Server{Addr: o.listen, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if o.otelEndpoint != "" {
		logger.Info("pushing metrics via otlp", "endpoint", o.otelEndpoint, "protocol", o.otelProtocol, "interval", o.otelInterval)
	}
	logger.Info("serving metrics", "listen", o.listen, "interval", o.interval)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
