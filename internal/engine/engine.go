package engine

import (
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/require"

	"github.com/CloudPassenger/miaoprobe/internal/logging"
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
func New(buildFetch FetchBuilder, logger *slog.Logger) (*goja.Runtime, error) {
	if logger == nil {
		logger = logging.Discard()
	}

	vm := goja.New()
	new(require.Registry).Enable(vm)
	console.Enable(vm)
	vm.SetMaxCallStackSize(1024)

	vm.Set("print", printFunc(logger))
	vm.Set("fetch", buildFetch(vm))

	if _, err := vm.RunString(predefinedJS); err != nil {
		return nil, fmt.Errorf("load predefined script: %w", err)
	}
	if _, err := vm.RunString("var module = { exports: {} }; var exports = module.exports;"); err != nil {
		return nil, fmt.Errorf("install commonjs shim: %w", err)
	}

	return vm, nil
}

// printFunc backs the script-visible println() global (via predefined.js's
// `const println = print;`), routing script output through the same
// structured logger as the rest of the engine instead of straight to
// stdout, so --log-format applies to it too.
func printFunc(logger *slog.Logger) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		args := make([]interface{}, len(call.Arguments))
		for i, a := range call.Arguments {
			args[i] = a
		}
		msg := strings.TrimRight(fmt.Sprintln(args...), "\n")
		logger.Info(msg, "source", "script")
		return goja.Undefined()
	}
}

// ExtraField is one entry of a script result's optional `extra` list: a
// display-ready key/value pair carrying its own label, type hint and unit so
// non-JSON renderers (the CLI table) can format it without guessing.
type ExtraField struct {
	Key   string      `json:"key"`
	Label string      `json:"label,omitempty"`
	Value interface{} `json:"value"`
	Type  string      `json:"type,omitempty"` // "string" (default), "number", "percent", "bool"
	Unit  string      `json:"unit,omitempty"`
}

// Result is the parsed return value of a script's handler function. Text and
// Background are the original miaospeed-scripts contract; Status, Region,
// Message, Error and Extra are miaoprobe-specific additions that a script
// may optionally return alongside them for richer display without breaking
// scripts that only set {text, background}.
type Result struct {
	Text       string
	Background string
	Status     string // "unlocked" | "failed" | "warning" | "unknown" | "na"; falls back to Background classification when empty
	Region     string // dynamically detected region, e.g. "US" (distinct from the script's static Regions config)
	Message    string // human-readable detail shown alongside Text
	Error      string // business-level error description (distinct from a Go-level execution error)
	Extra      []ExtraField
}

// ErrNoHandler is returned when a script exposes neither module.exports nor
// a global handler function.
var ErrNoHandler = errors.New("script does not export a handler function (expected module.exports or global handler)")

// RunScript executes the script source in vm, resolves its handler
// (module.exports, falling back to the global `handler`), invokes it, and
// parses the resulting {text, background, status, region, message, error,
// extra} object (all fields but text/background are optional). Both the
// top-level script evaluation and the handler call are bounded by timeout.
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
	return Result{
		Text:       getString(obj, "text"),
		Background: getString(obj, "background"),
		Status:     getString(obj, "status"),
		Region:     getString(obj, "region"),
		Message:    getString(obj, "message"),
		Error:      getString(obj, "error"),
		Extra:      parseExtra(obj.Get("extra")),
	}, nil
}

// getString reads obj[name] as a string, returning "" if the property is
// absent, null, undefined, or not a string. obj.Get returns a literal nil
// (not goja.Undefined()) for missing properties, so nil must be checked
// before calling Export.
func getString(obj *goja.Object, name string) string {
	v := obj.Get(name)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	s, _ := v.Export().(string)
	return s
}

// parseExtra converts a script's optional `extra` array (list of
// {key, label, value, type, unit} objects) into []ExtraField. Malformed or
// absent input yields nil rather than an error, since extra is always
// optional.
func parseExtra(v goja.Value) []ExtraField {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	items, ok := v.Export().([]interface{})
	if !ok {
		return nil
	}
	fields := make([]ExtraField, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		label, _ := m["label"].(string)
		typ, _ := m["type"].(string)
		unit, _ := m["unit"].(string)
		fields = append(fields, ExtraField{Key: key, Label: label, Value: m["value"], Type: typ, Unit: unit})
	}
	return fields
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
