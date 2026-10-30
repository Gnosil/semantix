package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"semantix/harness/netclient"
)

type capturedKeenableRequest struct {
	method      string
	path        string
	contentType string
	title       string
	apiKey      string
	body        map[string]any
}

func startKeenableServer(t *testing.T, status int, response string) (*httptest.Server, *capturedKeenableRequest) {
	t.Helper()
	got := &capturedKeenableRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		got.contentType = r.Header.Get("Content-Type")
		got.title = r.Header.Get("X-Keenable-Title")
		got.apiKey = r.Header.Get("X-API-Key")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func testKeenable(srv *httptest.Server, keyEnv string) keenableSearch {
	return keenableSearch{baseURL: srv.URL, apiKeyEnv: keyEnv, client: srv.Client()}
}

const testKeenableKeyEnv = "SEMANTIX_TEST_KEENABLE_KEY"

func TestKeenableBackendDefaults(t *testing.T) {
	b, err := newKeenableSearch(WebSearchOptions{Proxy: netclient.ProxySpec{Mode: netclient.ModeOff}})
	if err != nil {
		t.Fatal(err)
	}
	k := b.(keenableSearch)
	if k.baseURL != "https://api.keenable.ai" || k.apiKeyEnv != "KEENABLE_API_KEY" || k.client == nil {
		t.Fatalf("default backend = %+v", k)
	}
	b, err = newKeenableSearch(WebSearchOptions{APIKeyEnv: " MY_KEENABLE_KEY "})
	if err != nil || b.(keenableSearch).apiKeyEnv != "MY_KEENABLE_KEY" {
		t.Fatalf("api_key_env override = (%+v, %v)", b, err)
	}
}

func TestKeenableKeylessRequestShape(t *testing.T) {
	t.Setenv(testKeenableKeyEnv, "")
	srv, got := startKeenableServer(t, http.StatusOK, `{"results":[
		{"title":"Go 1.26 Release Notes","url":"https://go.dev/doc/go1.26","description":"","snippet":"Go 1.26 is out.","acquired_at":"2026-10-01"}
	]}`)

	results, err := testKeenable(srv, testKeenableKeyEnv).Search(context.Background(), "go 1.26 release", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/v1/search/public" || got.contentType != "application/json" || got.title != "Semantix" || got.apiKey != "" {
		t.Fatalf("keyless request = %+v", got)
	}
	if len(got.body) != 2 || got.body["query"] != "go 1.26 release" || got.body["max_results"] != float64(3) {
		t.Fatalf("request body = %v, want only query and max_results", got.body)
	}
	want := WebSearchResult{Title: "Go 1.26 Release Notes", URL: "https://go.dev/doc/go1.26", Text: "Go 1.26 is out."}
	if len(results) != 1 || results[0] != want {
		t.Fatalf("results = %+v, want snippet mapped to Text: %+v", results, want)
	}
}

func TestKeenableUsesKeyedEndpointWhenKeySet(t *testing.T) {
	t.Setenv(testKeenableKeyEnv, " test-key ")
	srv, got := startKeenableServer(t, http.StatusOK, `{"results":[]}`)

	results, err := testKeenable(srv, testKeenableKeyEnv).Search(context.Background(), "semantix", 5)
	if err != nil || len(results) != 0 {
		t.Fatalf("keyed search = (%v, %v), want no results", results, err)
	}
	if got.path != "/v1/search" || got.apiKey != "test-key" || got.title != "Semantix" {
		t.Fatalf("keyed request = %+v", got)
	}
}

func TestKeenableErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name     string
		key      string
		status   int
		body     string
		want     []string
		dontWant string
	}{
		{name: "keyless 429", status: http.StatusTooManyRequests, body: `{"detail":"rate limited"}`,
			want: []string{"status 429 Too Many Requests", "rate limited", "keyless rate limit; set " + testKeenableKeyEnv}},
		{name: "keyed 429", key: "k", status: http.StatusTooManyRequests, body: `{"detail":"rate limited"}`,
			want: []string{"status 429"}, dontWant: "keyless"},
		{name: "5xx", status: http.StatusBadGateway, body: strings.Repeat("x", 500),
			want: []string{"status 502 Bad Gateway: " + strings.Repeat("x", keenableMaxErrorBody)}, dontWant: strings.Repeat("x", keenableMaxErrorBody+1)},
		{name: "bad json", status: http.StatusOK, body: `<html>`, want: []string{"decode response"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(testKeenableKeyEnv, tc.key)
			srv, _ := startKeenableServer(t, tc.status, tc.body)
			_, err := testKeenable(srv, testKeenableKeyEnv).Search(context.Background(), "x", 5)
			if err == nil {
				t.Fatal("want an error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q missing %q", err, want)
				}
			}
			if tc.dontWant != "" && strings.Contains(err.Error(), tc.dontWant) {
				t.Fatalf("error %q should not contain %q", err, tc.dontWant)
			}
			if tc.key != "" && strings.Contains(err.Error(), tc.key) {
				t.Fatalf("error %q leaks the key", err)
			}
		})
	}
}

// The production client must not follow a redirect from the search endpoint,
// keyed or keyless: the destination (plain HTTP here, behind an HTTPS source)
// is never contacted, and the 3xx comes back as an error.
func TestKeenableNeverFollowsRedirects(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for _, key := range []string{"", "test-key"} {
			t.Run(fmt.Sprintf("%d keyed=%t", status, key != ""), func(t *testing.T) {
				t.Setenv(testKeenableKeyEnv, key)
				var contacted atomic.Bool
				dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					contacted.Store(true)
				}))
				t.Cleanup(dest.Close)
				src := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, dest.URL+r.URL.Path, status)
				}))
				t.Cleanup(src.Close)

				b, err := newKeenableSearch(WebSearchOptions{APIKeyEnv: testKeenableKeyEnv, Proxy: netclient.ProxySpec{Mode: netclient.ModeOff}})
				if err != nil {
					t.Fatal(err)
				}
				k := b.(keenableSearch)
				k.baseURL = src.URL
				k.client.Transport = src.Client().Transport // trust the test certificate; redirect policy unchanged

				_, err = k.Search(context.Background(), "private query", 5)
				if err == nil || !strings.HasPrefix(err.Error(), fmt.Sprintf("status %d ", status)) {
					t.Fatalf("err = %v, want the %d reported as an error", err, status)
				}
				if contacted.Load() {
					t.Fatal("redirect destination was contacted")
				}
			})
		}
	}
}

func TestKeenableThroughToolOnTimeoutAndUnreachable(t *testing.T) {
	t.Setenv(testKeenableKeyEnv, "")
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) }) // runs before slow.Close
	ws := webSearch{provider: "keenable", backend: testKeenable(slow, testKeenableKeyEnv), timeout: 50 * time.Millisecond}
	_, err := ws.Execute(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err == nil || err.Error() != "web_search (keenable): no response within 50ms" {
		t.Fatalf("timeout = %v", err)
	}

	down := keenableSearch{baseURL: "http://127.0.0.1:1", apiKeyEnv: testKeenableKeyEnv, client: http.DefaultClient}
	ws = webSearch{provider: "keenable", backend: down, timeout: webSearchTimeout}
	_, err = ws.Execute(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err == nil || !strings.HasPrefix(err.Error(), "web_search (keenable): ") {
		t.Fatalf("unreachable backend = %v", err)
	}
}
