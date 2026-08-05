package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dop251/goja"
)

func TestFetchFactoryDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://example.com/restrict.html")
		http.SetCookie(w, &http.Cookie{Name: "steamCountry", Value: "US"})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	vm := goja.New()
	fetch := FetchFactory(vm, FetchOptions{})
	vm.Set("fetch", fetch)

	v, err := vm.RunString(`
		var resp = fetch("` + srv.URL + `", {});
		var out = {};
		out.body = resp.body;
		out.statusCode = resp.statusCode;
		out.location = resp.headers["location"];
		out.cookieName = resp.cookies[0].Name;
		JSON.stringify(out);
	`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := v.String()
	want := `{"body":"hello","statusCode":200,"location":"https://example.com/restrict.html","cookieName":"steamCountry"}`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestFetchFactoryFailureReturnsNull(t *testing.T) {
	vm := goja.New()
	fetch := FetchFactory(vm, FetchOptions{})
	vm.Set("fetch", fetch)

	v, err := vm.RunString(`fetch("http://127.0.0.1:1", {retry: 1, timeout: 200}) === null`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !v.ToBoolean() {
		t.Fatal("expected fetch to return null on failure")
	}
}

// A script must not be able to stall a poll cycle past the clamp by asking
// for an enormous per-attempt timeout.
func TestClampTimeoutMs(t *testing.T) {
	cases := []struct{ in, want int64 }{
		{0, DefaultRequestTimeoutMs},
		{-1, DefaultRequestTimeoutMs},
		{500, 500},
		{MaxRequestTimeoutMs, MaxRequestTimeoutMs},
		{600000, MaxRequestTimeoutMs}, // 10 minutes requested -> clamped
	}
	for _, c := range cases {
		if got := clampTimeoutMs(c.in); got != c.want {
			t.Errorf("clampTimeoutMs(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// Cancelling the context must abort a fetch that is blocked on a hung
// server. This is the only mechanism that can stop such a script, since
// goja's interrupt cannot preempt a blocking Go call.
func TestFetchAbortsOnContextCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	vm := goja.New()
	vm.Set("fetch", FetchFactory(vm, FetchOptions{Context: ctx}))

	time.AfterFunc(150*time.Millisecond, cancel)

	start := time.Now()
	v, err := vm.RunString(`fetch("` + srv.URL + `", {retry: 10, timeout: 30000}) === null`)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !v.ToBoolean() {
		t.Fatal("expected fetch to return null after cancellation")
	}
	// Without ctx plumbing this would be 10 retries x 30s.
	if elapsed > 5*time.Second {
		t.Fatalf("fetch did not abort promptly on cancel: took %v", elapsed)
	}
}
