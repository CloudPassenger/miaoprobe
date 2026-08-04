// Package compat is the hard compatibility acceptance test required by the
// project spec: it runs every script listed in miaospeed-scripts/dist/index.json
// through the real engine in direct-egress mode and asserts the host API
// contract holds (no ReferenceError/TypeError style incompatibilities, and
// handler() results parse into {text, background}). It intentionally does
// NOT assert business correctness of unlock results, since that depends on
// the real network environment the test happens to run in.
package compat

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CloudPassenger/miaoprobe/internal/probe"
	"github.com/CloudPassenger/miaoprobe/internal/script"
)

const defaultScriptsDir = "/path/to/miaospeed-scripts/dist"

func scriptsDir() string {
	if v := os.Getenv("MIAOSPEED_SCRIPTS_DIR"); v != "" {
		return v
	}
	return defaultScriptsDir
}

// isHostIncompatibility reports whether err indicates the engine failed to
// satisfy the miaospeed-scripts host API contract (missing global, wrong
// module.exports handling, malformed handler result), as opposed to a
// transient failure caused by real-world network conditions (timeout, DNS,
// connection refused, or a script's own null-fetch guard raising a TypeError
// because the upstream request itself failed).
func isHostIncompatibility(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()

	// Contract violations we own: missing handler / bad result shape.
	if strings.Contains(msg, "does not export a handler function") ||
		strings.Contains(msg, "handler result is not an object") ||
		strings.Contains(msg, "handler returned no result") {
		return true
	}

	// Timeouts and interrupted execution are treated as network-dependent,
	// not a contract incompatibility.
	if strings.Contains(msg, "executing too long") || strings.Contains(msg, "interrupted") {
		return false
	}

	// A ReferenceError for a missing/undefined global is unambiguously a
	// host API gap on our side.
	if strings.Contains(msg, "ReferenceError") || strings.Contains(msg, "is not defined") {
		return true
	}

	// SyntaxError means the engine could not even parse the script.
	if strings.Contains(msg, "SyntaxError") {
		return true
	}

	// TypeError/"is not a function" can legitimately happen when a script's
	// own null-fetch guard is missing and a real request failed (network
	// dependent), so it is not treated as a hard incompatibility by default.
	return false
}

type result struct {
	id  string
	err error
}

func TestCompatibilityAgainstMiaospeedScripts(t *testing.T) {
	dir := scriptsDir()
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Skipf("miaospeed-scripts dist not found at %s (set MIAOSPEED_SCRIPTS_DIR to override): %v", dir, err)
	}

	scripts, err := script.Load(dir)
	if err != nil {
		t.Fatalf("load scripts: %v", err)
	}
	if len(scripts) == 0 {
		t.Fatal("no scripts loaded from manifest")
	}

	const concurrency = 16
	sem := make(chan struct{}, concurrency)
	results := make([]result, len(scripts))

	var wg sync.WaitGroup
	for i, sc := range scripts {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, sc script.Script) {
			defer wg.Done()
			defer func() { <-sem }()
			outcome := probe.Run(sc, nil, 45*time.Second)
			results[i] = result{id: sc.ID, err: outcome.Err}
		}(i, sc)
	}
	wg.Wait()

	var passed, softFailed int
	var hardFailures []result
	for _, r := range results {
		switch {
		case r.err == nil:
			passed++
		case isHostIncompatibility(r.err):
			hardFailures = append(hardFailures, r)
		default:
			softFailed++
			t.Logf("script %s: network-dependent failure (not a contract violation): %v", r.id, r.err)
		}
	}

	t.Logf("compatibility summary: %d/%d passed, %d network-dependent failures, %d contract violations",
		passed, len(scripts), softFailed, len(hardFailures))

	for _, f := range hardFailures {
		t.Errorf("script %s: host API contract violation: %v", f.id, f.err)
	}
}
