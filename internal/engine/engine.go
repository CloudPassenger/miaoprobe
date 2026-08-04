package engine

import (
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/require"
)

//go:embed predefined.js
var predefinedJS string

// FetchBuilder constructs the fetch() host function for a runtime. It is a
// callback (rather than a plain FetchFunc) because implementations such as
// network.FetchFactory need the *goja.Runtime to build return values.
type FetchBuilder func(vm *goja.Runtime) func(call goja.FunctionCall) goja.Value

// New creates a goja runtime with println/get/safeStringify/safeParse (loaded
// from the embedded predefined.js, ported verbatim from miaospeed) plus the
// fetch implementation from buildFetch and a minimal CommonJS module.exports
// shim.
func New(buildFetch FetchBuilder) (*goja.Runtime, error) {
	vm := goja.New()
	new(require.Registry).Enable(vm)
	console.Enable(vm)
	vm.SetMaxCallStackSize(1024)

	vm.Set("print", printFunc())
	vm.Set("fetch", buildFetch(vm))

	if _, err := vm.RunString(predefinedJS); err != nil {
		return nil, fmt.Errorf("load predefined script: %w", err)
	}
	if _, err := vm.RunString("var module = { exports: {} }; var exports = module.exports;"); err != nil {
		return nil, fmt.Errorf("install commonjs shim: %w", err)
	}

	return vm, nil
}

func printFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		args := make([]interface{}, len(call.Arguments))
		for i, a := range call.Arguments {
			args[i] = a
		}
		fmt.Println(args...)
		return goja.Undefined()
	}
}

// Result is the parsed return value of a script's handler function.
type Result struct {
	Text       string
	Background string
}

// ErrNoHandler is returned when a script exposes neither module.exports nor
// a global handler function.
var ErrNoHandler = errors.New("script does not export a handler function (expected module.exports or global handler)")

// RunScript executes the script source in vm, resolves its handler
// (module.exports, falling back to the global `handler`), invokes it, and
// parses the resulting {text, background} object. Both the top-level script
// evaluation and the handler call are bounded by timeout.
func RunScript(vm *goja.Runtime, source string, timeout time.Duration) (Result, error) {
	if _, err := RunWithTimeout(vm, timeout, func() (goja.Value, error) {
		return vm.RunString(source)
	}); err != nil {
		return Result{}, fmt.Errorf("execute script: %w", err)
	}

	handlerFn, err := resolveHandler(vm)
	if err != nil {
		return Result{}, err
	}

	ret, err := RunWithTimeout(vm, timeout, func() (goja.Value, error) {
		return handlerFn(goja.Undefined())
	})
	if err != nil {
		return Result{}, fmt.Errorf("run handler: %w", err)
	}

	return parseResult(vm, ret)
}

func resolveHandler(vm *goja.Runtime) (goja.Callable, error) {
	if moduleVal := vm.Get("module"); moduleVal != nil && !goja.IsUndefined(moduleVal) && !goja.IsNull(moduleVal) {
		if moduleObj, ok := moduleVal.(*goja.Object); ok {
			if fn, ok := goja.AssertFunction(moduleObj.Get("exports")); ok {
				return fn, nil
			}
		}
	}
	if fn, ok := goja.AssertFunction(vm.Get("handler")); ok {
		return fn, nil
	}
	return nil, ErrNoHandler
}

func parseResult(vm *goja.Runtime, v goja.Value) (Result, error) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return Result{}, errors.New("handler returned no result")
	}
	obj, ok := v.(*goja.Object)
	if !ok {
		return Result{}, errors.New("handler result is not an object")
	}
	text, _ := obj.Get("text").Export().(string)
	background, _ := obj.Get("background").Export().(string)
	return Result{Text: text, Background: background}, nil
}

// RunWithTimeout runs fn while enforcing timeout via vm.Interrupt; a
// non-positive timeout disables the deadline.
func RunWithTimeout(vm *goja.Runtime, timeout time.Duration, fn func() (goja.Value, error)) (ret goja.Value, err error) {
	if timeout <= 0 {
		return fn()
	}

	var mu sync.Mutex
	finished := false

	timer := time.AfterFunc(timeout, func() {
		mu.Lock()
		defer mu.Unlock()
		if !finished {
			finished = true
			vm.Interrupt("script executing too long")
		}
	})
	defer timer.Stop()

	ret, err = fn()

	mu.Lock()
	finished = true
	mu.Unlock()

	return
}
