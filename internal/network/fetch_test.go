package network

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
