package engine

import (
	"testing"
	"time"

	"github.com/dop251/goja"
)

func nullFetch(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		return goja.Null()
	}
}

func TestRunScriptModuleExports(t *testing.T) {
	vm, err := New(nullFetch, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	src := `
		module.exports = function() {
			println("hello", 1, get({a:1}, "a", 0));
			return { text: safeStringify({ok:true}), background: "186,230,126" };
		};
	`
	res, err := RunScript(vm, src, time.Second)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if res.Background != "186,230,126" {
		t.Fatalf("unexpected background: %q", res.Background)
	}
	if res.Text != `{"ok":true}` {
		t.Fatalf("unexpected text: %q", res.Text)
	}
}

func TestRunScriptGlobalHandlerFallback(t *testing.T) {
	vm, err := New(nullFetch, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	src := `function handler() { return { text: "ok", background: "92,207,230" }; }`
	res, err := RunScript(vm, src, time.Second)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if res.Text != "ok" || res.Background != "92,207,230" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestRunScriptTimeout(t *testing.T) {
	vm, err := New(nullFetch, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	src := `module.exports = function() { while (true) {} };`
	_, err = RunScript(vm, src, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestRunScriptNoHandler(t *testing.T) {
	vm, err := New(nullFetch, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = RunScript(vm, `var x = 1;`, time.Second)
	if err != ErrNoHandler {
		t.Fatalf("expected ErrNoHandler, got %v", err)
	}
}
