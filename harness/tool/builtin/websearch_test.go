package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"semantix/harness/netclient"
	"semantix/harness/tool"
)

// fakeSearchBackend records the query it receives and returns canned results.
type fakeSearchBackend struct {
	results []WebSearchResult
	err     error
	block   bool // wait for ctx to end, like a backend that never answers

	query      string
	maxResults int
}

func (f *fakeSearchBackend) Search(ctx context.Context, query string, maxResults int) ([]WebSearchResult, error) {
	f.query, f.maxResults = query, maxResults
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.results, f.err
}

func fakeWebSearch(b *fakeSearchBackend) webSearch {
	return webSearch{provider: "fake", backend: b, timeout: webSearchTimeout}
}

func TestConfiguredWebSearchIsOptIn(t *testing.T) {
	if got, err := ConfiguredWebSearch("", WebSearchOptions{}); err != nil || got != nil {
		t.Fatalf("empty provider = (%v, %v), want no tool", got, err)
	}
	if _, ok := tool.LookupBuiltin("web_search"); ok {
		t.Fatal("web_search must not be a default built-in")
	}
	_, err := ConfiguredWebSearch("bogus", WebSearchOptions{})
	if err == nil || !strings.Contains(err.Error(), "supported: keenable") {
		t.Fatalf("unknown provider error = %v, want the supported list", err)
	}
	if !slices.Contains(WebSearchProviders(), "keenable") {
		t.Fatalf("WebSearchProviders() = %v, want keenable registered", WebSearchProviders())
	}
}

func TestConfiguredWebSearchSelectsRegisteredBackend(t *testing.T) {
	const name = "test-backend"
	var gotOpts WebSearchOptions
	backend := &fakeSearchBackend{}
	RegisterWebSearchBackend(name, func(opts WebSearchOptions) (WebSearchBackend, error) {
		gotOpts = opts
		return backend, nil
	})
	t.Cleanup(func() { delete(webSearchBackends, name) })

	opts := WebSearchOptions{APIKeyEnv: "MY_SEARCH_KEY", Proxy: netclient.ProxySpec{Mode: netclient.ModeOff}}
	got, err := ConfiguredWebSearch(" Test-Backend ", opts)
	if err != nil || got == nil {
		t.Fatalf("registered provider = (%v, %v), want a tool", got, err)
	}
	ws := got.(webSearch)
	if ws.Name() != "web_search" || !ws.ReadOnly() || ws.provider != name || ws.backend != backend || ws.timeout != webSearchTimeout {
		t.Fatalf("configured tool = %+v", ws)
	}
	if !reflect.DeepEqual(gotOpts, opts) {
		t.Fatalf("factory options = %+v, want %+v", gotOpts, opts)
	}
	if _, ok := tool.Tool(ws).(tool.SnipHinter); !ok {
		t.Fatal("web_search must declare a snip policy")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("duplicate backend registration should panic")
		}
	}()
	RegisterWebSearchBackend(name, nil)
}

func TestConfiguredWebSearchReportsFactoryError(t *testing.T) {
	const name = "test-broken"
	RegisterWebSearchBackend(name, func(WebSearchOptions) (WebSearchBackend, error) {
		return nil, errors.New("bad proxy")
	})
	t.Cleanup(func() { delete(webSearchBackends, name) })
	if got, err := ConfiguredWebSearch(name, WebSearchOptions{}); got != nil || err == nil || !strings.Contains(err.Error(), "bad proxy") {
		t.Fatalf("factory failure = (%v, %v), want no tool and the cause", got, err)
	}
}

func TestWebSearchArgsAndFormatting(t *testing.T) {
	longText := strings.Repeat("word ", 300)
	backend := &fakeSearchBackend{results: []WebSearchResult{
		{Title: "Go 1.26 Release Notes", URL: "https://go.dev/doc/go1.26", Text: "Go 1.26\n\n  is out."},
		{URL: "https://example.com/long", Text: longText},
	}}
	out, err := fakeWebSearch(backend).Execute(context.Background(), json.RawMessage(`{"query":" go 1.26 release ","max_results":50}`))
	if err != nil {
		t.Fatal(err)
	}
	if backend.query != "go 1.26 release" || backend.maxResults != webSearchMaxResults {
		t.Fatalf("backend got (%q, %d), want trimmed query and max_results clamped to %d", backend.query, backend.maxResults, webSearchMaxResults)
	}
	for _, want := range []string{
		`2 results for "go 1.26 release"`,
		"1. Go 1.26 Release Notes\n   https://go.dev/doc/go1.26\n   Go 1.26 is out.",
		"2. https://example.com/long\n   https://example.com/long\n   word word",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "…") || len([]rune(out)) > 1000 {
		t.Fatalf("long excerpt should be capped with an ellipsis, got %d runes", len([]rune(out)))
	}

	// Default count, empty result, and a backend that over-delivers.
	backend = &fakeSearchBackend{}
	out, err = fakeWebSearch(backend).Execute(context.Background(), json.RawMessage(`{"query":"semantix"}`))
	if err != nil || out != `no results for "semantix"` || backend.maxResults != webSearchDefaultResults {
		t.Fatalf("empty search = (%q, %v, max %d), want no-results text and default %d", out, err, backend.maxResults, webSearchDefaultResults)
	}
	backend = &fakeSearchBackend{results: make([]WebSearchResult, 4)}
	out, err = fakeWebSearch(backend).Execute(context.Background(), json.RawMessage(`{"query":"x","max_results":2}`))
	if err != nil || !strings.HasPrefix(out, `2 results for "x"`) {
		t.Fatalf("over-delivering backend = (%q, %v), want 2 results", out, err)
	}
}

func TestWebSearchErrorsBecomeToolErrors(t *testing.T) {
	backend := &fakeSearchBackend{}
	for _, args := range []string{`{"query":"  "}`, `{"query":`} {
		if _, err := fakeWebSearch(backend).Execute(context.Background(), json.RawMessage(args)); err == nil {
			t.Fatalf("args %s: want an error", args)
		}
	}
	if backend.query != "" {
		t.Fatal("invalid args must not reach the backend")
	}

	_, err := fakeWebSearch(&fakeSearchBackend{err: errors.New("status 503 Service Unavailable: down")}).Execute(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err == nil || err.Error() != "web_search (fake): status 503 Service Unavailable: down" {
		t.Fatalf("backend error = %v, want it wrapped with the provider name", err)
	}

	ws := fakeWebSearch(&fakeSearchBackend{block: true})
	ws.timeout = 20 * time.Millisecond
	_, err = ws.Execute(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "web_search (fake): no response within 20ms") {
		t.Fatalf("timeout error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = fakeWebSearch(&fakeSearchBackend{block: true}).Execute(ctx, json.RawMessage(`{"query":"x"}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled turn error = %v, want context.Canceled", err)
	}
}
