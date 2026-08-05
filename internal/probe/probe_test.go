package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CloudPassenger/miaoprobe/internal/script"
)

// The per-probe timeout must actually bound a script blocked in fetch().
// goja's interrupt alone cannot do this, so before the context was threaded
// through the network layer this ran for the server's full duration.
func TestRunTimeoutBoundsBlockingFetch(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	sc := script.Script{
		ID: "hang",
		Source: `module.exports = function() {
			fetch("` + srv.URL + `", {retry: 10, timeout: 30000});
			return {text: "ok", status: "unlocked"};
		};`,
	}

	start := time.Now()
	out := Run(context.Background(), sc, nil, 1*time.Second, nil)
	elapsed := time.Since(start)

	// retry:10 x timeout:30s would be 300s unbounded; clamped it is still
	// 10 x 30s. Only the probe deadline brings this back to ~1s.
	if elapsed > 5*time.Second {
		t.Fatalf("probe timeout did not bound a blocking fetch: took %v", elapsed)
	}
	t.Logf("bounded at %v (outcome err=%v)", elapsed, out.Err)
}

// Cancelling the parent context (process shutdown) must unblock a probe
// promptly rather than letting it run to completion.
func TestRunHonorsParentCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	sc := script.Script{
		ID:     "hang",
		Source: `module.exports = function() { fetch("` + srv.URL + `", {retry: 10, timeout: 30000}); return {text:"ok"}; };`,
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	start := time.Now()
	Run(ctx, sc, nil, 5*time.Minute, nil)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("probe ignored parent cancellation: took %v", elapsed)
	}
}

// The Runner must compile each script once and produce identical results to
// the uncached path.
func TestRunnerCachesAndMatchesRun(t *testing.T) {
	sc := script.Script{
		ID:     "ok",
		Source: `module.exports = function() { return {text: "hello", status: "unlocked"}; };`,
	}
	r := NewRunner([]script.Script{sc}, nil)
	if len(r.programs) != 1 {
		t.Fatalf("expected 1 cached program, got %d", len(r.programs))
	}

	for range 3 {
		out := r.Run(context.Background(), sc, nil, time.Second, nil)
		if out.Err != nil {
			t.Fatalf("runner: %v", out.Err)
		}
		if out.Result.Text != "hello" || out.Result.Status != "unlocked" {
			t.Fatalf("unexpected result: %+v", out.Result)
		}
	}
}

// A script that fails to compile must be dropped from the runner rather
// than taking down the service, and must still be probe-able (and report
// its error) via the fallback path.
func TestRunnerSkipsUncompilableScript(t *testing.T) {
	bad := script.Script{ID: "bad", Source: `module.exports = function( {{{ syntax error`}
	good := script.Script{ID: "good", Source: `module.exports = function() { return {text:"ok"}; };`}

	r := NewRunner([]script.Script{bad, good}, nil)
	if _, ok := r.programs["bad"]; ok {
		t.Fatal("uncompilable script should not be cached")
	}
	if _, ok := r.programs["good"]; !ok {
		t.Fatal("valid script should be cached")
	}

	if out := r.Run(context.Background(), bad, nil, time.Second, nil); out.Err == nil {
		t.Fatal("expected an error for the uncompilable script")
	}
	if out := r.Run(context.Background(), good, nil, time.Second, nil); out.Err != nil {
		t.Fatalf("valid script errored: %v", out.Err)
	}
}

// Each probe must get a clean runtime: reusing one would let a script that
// does not reassign module.exports inherit the previous script's handler.
func TestEachProbeGetsIsolatedRuntime(t *testing.T) {
	first := script.Script{ID: "first", Source: `leaked = "from-first"; module.exports = function() { return {text: "first"}; };`}
	second := script.Script{ID: "second", Source: `function handler() { return {text: typeof leaked === "undefined" ? "clean" : "LEAKED"}; }`}

	r := NewRunner([]script.Script{first, second}, nil)
	if out := r.Run(context.Background(), first, nil, time.Second, nil); out.Result.Text != "first" {
		t.Fatalf("unexpected first result: %+v", out.Result)
	}
	out := r.Run(context.Background(), second, nil, time.Second, nil)
	if out.Err != nil {
		t.Fatalf("second: %v", out.Err)
	}
	if out.Result.Text != "clean" {
		t.Fatalf("state leaked between probes: %+v", out.Result)
	}
}
