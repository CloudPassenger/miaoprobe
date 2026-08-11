// Package probe wires together internal/engine, internal/network and
// internal/script to execute one script once. It is shared by the check
// subcommand (one-shot) and the serve subcommand's scheduler.
package probe

import (
	"context"
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
	Script     script.Script
	Result     engine.Result
	Duration   time.Duration
	Err        error
	RequestErr error
}

// Run builds a fresh goja VM with fetch() bound to proxy (nil = direct
// egress) and executes sc's handler, bounded by timeout. Every log line
// produced while running sc (fetch activity, script println output) is
// tagged with a "script" attribute for correlation.
//
// The script source is compiled on each call; callers that run the same
// scripts repeatedly should use a Runner instead, which caches the compiled
// program.
func Run(ctx context.Context, sc script.Script, proxy *network.ProxyConfig, timeout time.Duration, logger *slog.Logger) Outcome {
	prog, err := engine.Compile(sc.ID+".js", sc.Source)
	if err != nil {
		return Outcome{Script: sc, Err: err}
	}
	return run(ctx, sc, prog, proxy, timeout, logger)
}

// Runner executes scripts repeatedly, caching each script's compiled
// program so the source is parsed once rather than on every poll.
//
// A Runner is safe for concurrent use: the cache is populated up front by
// NewRunner and only read afterwards, and each Run still builds its own
// goja.Runtime (which is not goroutine-safe and must not be shared).
type Runner struct {
	programs map[string]*goja.Program
}

// NewRunner compiles every script once. A script that fails to compile is
// reported in the returned error and excluded from the runner, so one bad
// script cannot take down the whole service.
func NewRunner(scripts []script.Script, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = logging.Discard()
	}
	programs := make(map[string]*goja.Program, len(scripts))
	for _, sc := range scripts {
		prog, err := engine.Compile(sc.ID+".js", sc.Source)
		if err != nil {
			logger.Error("script failed to compile, it will be skipped", "script", sc.ID, "err", err)
			continue
		}
		programs[sc.ID] = prog
	}
	return &Runner{programs: programs}
}

// Run executes sc using the cached program, falling back to compiling on
// demand for a script the Runner has not seen.
func (r *Runner) Run(ctx context.Context, sc script.Script, proxy *network.ProxyConfig, timeout time.Duration, logger *slog.Logger) Outcome {
	prog, ok := r.programs[sc.ID]
	if !ok {
		return Run(ctx, sc, proxy, timeout, logger)
	}
	return run(ctx, sc, prog, proxy, timeout, logger)
}

func run(ctx context.Context, sc script.Script, prog *goja.Program, proxy *network.ProxyConfig, timeout time.Duration, logger *slog.Logger) Outcome {
	if logger == nil {
		logger = logging.Discard()
	}
	scLogger := logger.With("script", sc.ID)
	start := time.Now()

	scLogger.Debug("running script")

	// Bound the probe with a real deadline rather than relying on
	// vm.Interrupt alone: goja only checks the interrupt flag between
	// bytecode instructions, so a script blocked inside fetch() is
	// unreachable by it. Cancelling this context is what actually aborts
	// the in-flight HTTP request.
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var requestErr error

	vm, err := engine.New(func(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
		return network.FetchFactory(vm, network.FetchOptions{
			Proxy: proxy, Logger: scLogger, Context: ctx,
			ReportFailure: func(err error) { requestErr = err },
		})
	}, scLogger)
	if err != nil {
		scLogger.Error("failed to initialize engine", "err", err)
		return Outcome{Script: sc, Err: err, RequestErr: requestErr, Duration: time.Since(start)}
	}

	res, err := engine.RunProgram(vm, prog, timeout)
	duration := time.Since(start)
	if err != nil {
		scLogger.Error("script execution failed", "duration", duration, "err", err)
	} else {
		scLogger.Debug("script finished", "duration", duration, "status", res.Status, "background", res.Background, "region", res.Region)
	}
	return Outcome{Script: sc, Result: res, Err: err, RequestErr: requestErr, Duration: duration}
}
