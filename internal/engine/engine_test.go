package engine

import (
	"errors"
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

func TestRunScriptExtendedFields(t *testing.T) {
	vm, err := New(nullFetch, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	src := `
		module.exports = function() {
			return {
				text: "US (Originals Only)",
				background: "186,230,126",
				status: "warning",
				statusReason: "originals_only",
				region: "US",
				message: "matched via homepage redirect",
				extra: [
					{ key: "ip_quality", label: "IP质量评分", value: 87, type: "number", unit: "分" },
					{ key: "asn", label: "ASN", value: "AS13335" },
				],
			};
		};
	`
	res, err := RunScript(vm, src, time.Second)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if res.Status != "warning" || res.StatusReason != "originals_only" || res.Region != "US" || res.Message == "" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(res.Extra) != 2 || res.Extra[0].Key != "ip_quality" || res.Extra[0].Value != int64(87) {
		t.Fatalf("unexpected extra: %+v", res.Extra)
	}
}

func TestRunScriptMinimalFieldsStillWork(t *testing.T) {
	vm, err := New(nullFetch, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	src := `module.exports = function() { return { text: "ok", background: "239,107,115" }; };`
	res, err := RunScript(vm, src, time.Second)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if res.Status != "" || res.StatusReason != "" || res.Region != "" || res.Message != "" || res.Error != "" || res.Extra != nil {
		t.Fatalf("expected zero-value optional fields, got %+v", res)
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
	if !errors.Is(err, ErrNoHandler) {
		t.Fatalf("expected ErrNoHandler, got %v", err)
	}
}
