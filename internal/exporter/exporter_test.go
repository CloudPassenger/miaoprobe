package exporter

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"

	"github.com/CloudPassenger/miaoprobe/internal/otelsetup"
	"github.com/CloudPassenger/miaoprobe/internal/probe"
	"github.com/CloudPassenger/miaoprobe/internal/script"
)

// newTestPoller wires a Poller to a real Prometheus-backed MeterProvider so
// tests can assert on the exact text that /metrics would serve. The returned
// poll func runs exactly one cycle (the setup Run does before its ticker
// loop) against a live context.
func newTestPoller(t *testing.T, scripts []script.Script) (*Poller, func() string) {
	t.Helper()

	provider, err := otelsetup.New(t.Context(), otelsetup.Config{ServiceName: "miaoprobe-test"})
	if err != nil {
		t.Fatalf("otelsetup.New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = provider.Shutdown(ctx)
	})

	metrics, err := NewMetrics(provider.MeterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	p := &Poller{
		Scripts:  scripts,
		Timeout:  5 * time.Second,
		Interval: time.Hour,
		Metrics:  metrics,
	}

	scrape := func() string {
		mfs, err := provider.Registry.Gather()
		if err != nil {
			t.Fatalf("Gather: %v", err)
		}
		var sb strings.Builder
		enc := expfmt.NewEncoder(&sb, expfmt.NewFormat(expfmt.TypeTextPlain))
		for _, mf := range mfs {
			if err := enc.Encode(mf); err != nil {
				t.Fatalf("encode: %v", err)
			}
		}
		return sb.String()
	}
	return p, scrape
}

func mustLine(t *testing.T, scrape string, want string) {
	t.Helper()
	if !strings.Contains(scrape, want) {
		t.Fatalf("expected metrics to contain %q, got:\n%s", want, scrape)
	}
}

// A script that errors must overwrite its previous status rather than leave
// a stale "unlocked" behind, otherwise an outage is invisible to alerts.
func TestErroringScriptOverwritesStaleStatus(t *testing.T) {
	sc := script.Script{
		ID:     "flaky",
		Name:   "Flaky",
		Source: `module.exports = function() { return {text: "ok", status: "unlocked"}; };`,
	}
	p, scrape := newTestPoller(t, []script.Script{sc})

	pollCycle(t, p)
	mustLine(t, scrape(), `miaoprobe_unlock_status{id="flaky"} 1`)

	// Now the same script starts failing to compile/run.
	p.Scripts = []script.Script{{ID: "flaky", Name: "Flaky", Source: `module.exports = function() { throw new Error("boom"); };`}}
	p.runner = nil // force recompile of the replaced source
	pollCycle(t, p)

	out := scrape()
	mustLine(t, out, `miaoprobe_unlock_status{id="flaky"} -1`)
	if strings.Contains(out, `miaoprobe_unlock_status{id="flaky"} 1`) {
		t.Fatalf("stale unlocked value still served:\n%s", out)
	}
}

// A script reporting n/a previously wrote no metric at all, freezing the
// previous value forever. It must now report unknown.
func TestNAStatusRecordsUnknownInsteadOfSkipping(t *testing.T) {
	sc := script.Script{
		ID:     "nascript",
		Name:   "NA",
		Source: `module.exports = function() { return {text: "n/a", status: "na"}; };`,
	}
	p, scrape := newTestPoller(t, []script.Script{sc})
	pollCycle(t, p)

	mustLine(t, scrape(), `miaoprobe_unlock_status{id="nascript"} -1`)
}

// Metadata must live on miaoprobe_script_info, so that editing a script's
// region/tags cannot orphan a status series (gauge attribute sets are never
// forgotten by the SDK).
func TestStatusSeriesCarriesOnlyIDLabel(t *testing.T) {
	sc := script.Script{
		ID:       "netflix",
		Name:     "Netflix",
		Category: "media",
		Regions:  []string{"us"},
		Tags:     []string{"media"},
		Source:   `module.exports = function() { return {text: "ok", status: "unlocked"}; };`,
	}
	p, scrape := newTestPoller(t, []script.Script{sc})
	pollCycle(t, p)

	out := scrape()
	mustLine(t, out, `miaoprobe_unlock_status{id="netflix"} 1`)
	mustLine(t, out, `miaoprobe_script_info{category="media",id="netflix",name="Netflix",region="us",tags="media"} 1`)

	// Simulate a metadata edit; the status series must not fork.
	p.Scripts[0].Regions = []string{"jp"}
	pollCycle(t, p)

	out = scrape()
	if n := strings.Count(out, `miaoprobe_unlock_status{`); n != 1 {
		t.Fatalf("expected exactly 1 unlock_status series after metadata change, got %d:\n%s", n, out)
	}
}

// LastSuccess is what lets operators alert on staleness rather than
// trusting a possibly-frozen status value.
func TestLastSuccessRecordedOnSuccessOnly(t *testing.T) {
	ok := script.Script{ID: "ok", Source: `module.exports = function() { return {text: "ok", status: "unlocked"}; };`}
	bad := script.Script{ID: "bad", Source: `module.exports = function() { throw new Error("boom"); };`}

	p, scrape := newTestPoller(t, []script.Script{ok, bad})
	pollCycle(t, p)

	out := scrape()
	mustLine(t, out, `miaoprobe_last_success_timestamp_seconds{id="ok"}`)
	if strings.Contains(out, `miaoprobe_last_success_timestamp_seconds{id="bad"}`) {
		t.Fatalf("errored script must not report a success timestamp:\n%s", out)
	}
	mustLine(t, out, `miaoprobe_check_errors_total{id="bad"} 1`)
}

// pollCycle drives exactly one polling cycle with a live context, mirroring
// what Poller.Run does before entering its ticker loop.
func pollCycle(t *testing.T, p *Poller) {
	t.Helper()
	if p.runner == nil {
		p.runner = probe.NewRunner(p.Scripts, nil)
	}
	p.publishScriptInfo(t.Context())
	p.pollOnce(t.Context())
}
