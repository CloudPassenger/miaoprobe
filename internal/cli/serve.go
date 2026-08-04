package cli

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/CloudPassenger/miaoprobe/internal/exporter"
	"github.com/CloudPassenger/miaoprobe/internal/network"
	"github.com/CloudPassenger/miaoprobe/internal/script"
)

func newServeCommand() *cobra.Command {
	var scriptsPath, proxyRaw, filterRaw, listen string
	var interval, timeout time.Duration

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Poll scripts on an interval and expose results as Prometheus metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(scriptsPath, proxyRaw, filterRaw, listen, interval, timeout)
		},
	}

	cmd.Flags().StringVar(&scriptsPath, "scripts", "", "directory containing index.json (required)")
	cmd.Flags().StringVar(&proxyRaw, "proxy", "", "egress proxy: http://host:port or socks5://host:port (empty = direct)")
	cmd.Flags().StringVar(&filterRaw, "filter", "", "comma-separated region/tag filter")
	cmd.Flags().DurationVar(&interval, "interval", 5*time.Minute, "polling interval")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "per-script execution timeout")
	cmd.Flags().StringVar(&listen, "listen", ":9765", "address to expose /metrics on")
	_ = cmd.MarkFlagRequired("scripts")

	return cmd
}

func runServe(scriptsPath, proxyRaw, filterRaw, listen string, interval, timeout time.Duration) error {
	proxyCfg, err := network.ParseProxy(proxyRaw)
	if err != nil {
		return err
	}

	scripts, err := script.Load(scriptsPath)
	if err != nil {
		return err
	}
	scripts = script.Filter(scripts, script.ParseFilter(filterRaw))
	log.Printf("loaded %d script(s) from %s", len(scripts), scriptsPath)

	reg := prometheus.NewRegistry()
	metrics := exporter.NewMetrics(reg)

	poller := &exporter.Poller{
		Scripts:  scripts,
		Proxy:    proxyCfg,
		Timeout:  timeout,
		Interval: interval,
		Metrics:  metrics,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go poller.Run(ctx)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	srv := &http.Server{Addr: listen, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("serving /metrics on %s (interval=%s)", listen, interval)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
