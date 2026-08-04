// Package probe wires together internal/engine, internal/network and
// internal/script to execute one script once. It is shared by the check
// subcommand (one-shot) and the serve subcommand's scheduler.
package probe

import (
	"time"

	"github.com/dop251/goja"

	"github.com/CloudPassenger/miaoprobe/internal/engine"
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
// egress) and executes sc's handler, bounded by timeout.
func Run(sc script.Script, proxy *network.ProxyConfig, timeout time.Duration) Outcome {
	start := time.Now()

	vm, err := engine.New(func(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
		return network.FetchFactory(vm, network.FetchOptions{Proxy: proxy})
	})
	if err != nil {
		return Outcome{Script: sc, Err: err, Duration: time.Since(start)}
	}

	res, err := engine.RunScript(vm, sc.Source, timeout)
	return Outcome{Script: sc, Result: res, Err: err, Duration: time.Since(start)}
}
