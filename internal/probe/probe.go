// Package probe wires together internal/engine, internal/network and
// internal/script to execute one script once. It is shared by the check
// subcommand (one-shot) and the serve subcommand's scheduler.
package probe

import (
	"log/slog"
	"time"

	"github.com/dop251/goja"

	"github.com/CloudPassenger/miaoprobe/internal/engine"
	"github.com/CloudPassenger/miaoprobe/internal/logging"
	"github.com/CloudPassenger/miaoprobe/internal/network"
	"github.com/CloudPassenger/miaoprobe/internal/script"
)

// Outcome is the result of running one script's handler once.
type Outcome struct {
	Script   script.Script
	Result   engine.Result
	Duration time.Duration
	Err      error
}

// Run builds a fresh goja VM with fetch() bound to proxy (nil = direct
// egress) and executes sc's handler, bounded by timeout. Every log line
// produced while running sc (fetch activity, script println output) is
// tagged with a "script" attribute for correlation.
func Run(sc script.Script, proxy *network.ProxyConfig, timeout time.Duration, logger *slog.Logger) Outcome {
	if logger == nil {
		logger = logging.Discard()
	}
	scLogger := logger.With("script", sc.ID)
	start := time.Now()

	scLogger.Debug("running script")

	vm, err := engine.New(func(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
		return network.FetchFactory(vm, network.FetchOptions{Proxy: proxy, Logger: scLogger})
	}, scLogger)
	if err != nil {
		scLogger.Error("failed to initialize engine", "err", err)
		return Outcome{Script: sc, Err: err, Duration: time.Since(start)}
	}

	res, err := engine.RunScript(vm, sc.Source, timeout)
	duration := time.Since(start)
	if err != nil {
		scLogger.Error("script execution failed", "duration", duration, "err", err)
	} else {
		scLogger.Debug("script finished", "duration", duration, "background", res.Background)
	}
	return Outcome{Script: sc, Result: res, Err: err, Duration: duration}
}
