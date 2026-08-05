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
