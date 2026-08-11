package exporter

import (
	"context"
	"net/http/httptest"
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

func TestProbeResultInfoExportsCurrentRegionAndExtra(t *testing.T) {
	sc := script.Script{
		ID: "netflix",
		Source: `module.exports = function() {
			return {
				text: "ok",
				status: "unlocked",
				region: "US",
				extra: [
					{key: "ip_quality", label: "IP quality", value: "residential", type: "string"},
					{key: "score", label: "Score", value: 98, type: "percent", unit: "%"}
				]
			};
		};`,
	}
	p, scrape := newTestPoller(t, []script.Script{sc})
	pollCycle(t, p)

	out := scrape()
	mustLine(t, out, `miaoprobe_probe_result_info{id="netflix",region="US"} 1`)
	mustLine(t, out, `miaoprobe_probe_extra_info{id="netflix",key="ip_quality",label="IP quality",region="US",type="string",unit="",value="residential"} 1`)
	mustLine(t, out, `miaoprobe_probe_extra_info{id="netflix",key="score",label="Score",region="US",type="percent",unit="%",value="98"} 1`)

	// Observable gauges emit only the latest snapshot, preventing a changed
	// quality value from leaving an old series behind.
	p.Scripts[0].Source = `module.exports = function() {
		return {text: "ok", status: "unlocked", region: "JP", extra: [{key: "ip_quality", value: "datacenter"}]};
	};`
	p.runner = nil
	pollCycle(t, p)

	out = scrape()
	mustLine(t, out, `miaoprobe_probe_result_info{id="netflix",region="JP"} 1`)
	mustLine(t, out, `miaoprobe_probe_extra_info{id="netflix",key="ip_quality",label="",region="JP",type="",unit="",value="datacenter"} 1`)
	if strings.Contains(out, `value="residential"`) || strings.Contains(out, `key="score"`) {
		t.Fatalf("stale extra metrics still served:\n%s", out)
	}
}

func TestProbeFailureInfoReplacesAndClearsCurrentSnapshot(t *testing.T) {
	sc := script.Script{
		ID:     "netflix",
		Source: `module.exports = function() { return {text: "failed", status: "failed"}; };`,
	}
	p, scrape := newTestPoller(t, []script.Script{sc})

	pollCycle(t, p)
	out := scrape()
	mustLine(t, out, `miaoprobe_probe_failure_info{class="availability",id="netflix",reason="region_unavailable"} 1`)

	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	p.Scripts[0].Source = `module.exports = function() {
		fetch("` + url + `", {retry: 1, timeout: 200});
		return {text: "failed", status: "failed", error: "\u7f51\u7edc"};
	};`
	p.runner = nil
	pollCycle(t, p)
	out = scrape()
	mustLine(t, out, `miaoprobe_probe_failure_info{class="network",id="netflix",reason="connection_refused"} 1`)
	if strings.Contains(out, `reason="region_unavailable"`) {
		t.Fatalf("stale availability failure still served:\n%s", out)
	}

	p.Scripts[0].Source = `module.exports = function() { return {text: "ok", status: "unlocked"}; };`
	p.runner = nil
	pollCycle(t, p)
	out = scrape()
	if strings.Contains(out, `miaoprobe_probe_failure_info{id="netflix"`) ||
		strings.Contains(out, `id="netflix",reason=`) {
		t.Fatalf("failure metric was not cleared after success:\n%s", out)
	}
}

func TestProbeWarningInfoReplacesAndClearsCurrentSnapshot(t *testing.T) {
	sc := script.Script{
		ID:     "netflix",
		Source: `module.exports = function() { return {text: "originals", status: "warning", statusReason: "originals_only"}; };`,
	}
	p, scrape := newTestPoller(t, []script.Script{sc})

	pollCycle(t, p)
	out := scrape()
	mustLine(t, out, `miaoprobe_probe_warning_info{class="restriction",id="netflix",reason="originals_only"} 1`)

	p.Scripts[0].Source = `module.exports = function() { return {text: "blocked", status: "warning", statusReason: "waf_blocked"}; };`
	p.runner = nil
	pollCycle(t, p)
	out = scrape()
	mustLine(t, out, `miaoprobe_probe_warning_info{class="access",id="netflix",reason="waf_blocked"} 1`)
	if strings.Contains(out, `reason="originals_only"`) {
		t.Fatalf("stale restriction warning still served:\n%s", out)
	}

	p.Scripts[0].Source = `module.exports = function() { return {text: "ok", status: "unlocked"}; };`
	p.runner = nil
	pollCycle(t, p)
	out = scrape()
	if strings.Contains(out, `miaoprobe_probe_warning_info{`) {
		t.Fatalf("warning metric was not cleared after success:\n%s", out)
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

// The whole point of the worker pool: N slow scripts must not take N *
// duration when run with concurrency.
func TestPollOnceRunsScriptsConcurrently(t *testing.T) {
	const n = 8
	scripts := make([]script.Script, 0, n)
	for i := range n {
		scripts = append(scripts, script.Script{
			ID: string(rune('a'+i)) + "-slow",
			// Busy-wait ~150ms in JS (no network needed).
			Source: `module.exports = function() {
				var end = Date.now() + 150;
				while (Date.now() < end) {}
				return {text: "ok", status: "unlocked"};
			};`,
		})
	}

	p, scrape := newTestPoller(t, scripts)
	p.Concurrency = n

	start := time.Now()
	pollCycle(t, p)
	elapsed := time.Since(start)

	if elapsed > 900*time.Millisecond {
		t.Fatalf("expected concurrent execution, took %v for %d scripts of ~150ms", elapsed, n)
	}
	if got := strings.Count(scrape(), `miaoprobe_unlock_status{`); got != n {
		t.Fatalf("expected %d status series, got %d", n, got)
	}
}

// A panic in one script must not kill the poller.
func TestPollerSurvivesScriptPanic(t *testing.T) {
	p, scrape := newTestPoller(t, []script.Script{
		{ID: "boom", Source: `module.exports = function() { null.x; };`},
		{ID: "fine", Source: `module.exports = function() { return {text: "ok", status: "unlocked"}; };`},
	})
	pollCycle(t, p)

	out := scrape()
	mustLine(t, out, `miaoprobe_unlock_status{id="fine"} 1`)
	mustLine(t, out, `miaoprobe_unlock_status{id="boom"} -1`)
}

func TestConcurrencyDefaults(t *testing.T) {
	scripts := make([]script.Script, 32)
	cases := []struct{ set, want int }{
		{0, DefaultConcurrency},
		{-5, DefaultConcurrency},
		{3, 3},
		{100, 32}, // capped at script count
	}
	for _, c := range cases {
		p := &Poller{Scripts: scripts, Concurrency: c.set}
		if got := p.concurrency(); got != c.want {
			t.Errorf("Concurrency=%d -> %d, want %d", c.set, got, c.want)
		}
	}
	empty := &Poller{}
	if got := empty.concurrency(); got != 1 {
		t.Errorf("empty poller concurrency = %d, want 1", got)
	}
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
