package network

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForConnectivityRetriesUntilReady(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := waitForConnectivity(context.Background(), nil, srv.URL, time.Second, nil, connectivityCheckOptions{
		retryInterval:  10 * time.Millisecond,
		attemptTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("waitForConnectivity: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestWaitForConnectivityUsesConfiguredHTTPProxy(t *testing.T) {
	var requests atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Host != "readiness.invalid" {
			t.Errorf("proxy request host = %q, want readiness.invalid", r.URL.Host)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxyServer.Close()

	cfg, err := ParseProxy(proxyServer.URL)
	if err != nil {
		t.Fatalf("ParseProxy: %v", err)
	}
	err = waitForConnectivity(context.Background(), cfg, "http://readiness.invalid/generate_204", time.Second, nil, connectivityCheckOptions{
		retryInterval:  10 * time.Millisecond,
		attemptTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("waitForConnectivity: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("proxy requests = %d, want 1", got)
	}
}

func TestWaitForConnectivityTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := waitForConnectivity(context.Background(), nil, srv.URL, 50*time.Millisecond, nil, connectivityCheckOptions{
		retryInterval:  10 * time.Millisecond,
		attemptTimeout: 20 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "startup connectivity check failed") {
		t.Fatalf("error = %v, want startup timeout", err)
	}
}

func TestWaitForConnectivityDisabled(t *testing.T) {
	if err := WaitForConnectivity(context.Background(), nil, "://invalid", 0, nil); err != nil {
		t.Fatalf("disabled connectivity check returned %v", err)
	}
}

func TestWaitForConnectivityHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForConnectivity(ctx, nil, "http://127.0.0.1:1", time.Second, nil, connectivityCheckOptions{
		retryInterval:  10 * time.Millisecond,
		attemptTimeout: 20 * time.Millisecond,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
