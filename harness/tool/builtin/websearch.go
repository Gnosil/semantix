package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"semantix/harness/netclient"
	"semantix/harness/tool"
)

// WebSearchResult is one ranked hit returned by a WebSearchBackend.
type WebSearchResult struct {
	Title string
	URL   string
	Text  string // page excerpt; the tool collapses whitespace and caps it
}

// WebSearchBackend runs one query for the client-side web_search tool. A failed
// request (transport, timeout, non-2xx status, undecodable body) returns an
// error, which the agent hands back to the model as an error tool result. An
// empty slice means the query matched nothing.
type WebSearchBackend interface {
	Search(ctx context.Context, query string, maxResults int) ([]WebSearchResult, error)
}

// WebSearchOptions carries the backend-neutral [tools.web_search] settings.
type WebSearchOptions struct {
	// APIKeyEnv names the environment variable holding an optional API key;
	// empty selects the backend's default name.
	APIKeyEnv string
	Proxy     netclient.ProxySpec
}

// WebSearchBackendFactory builds a backend from [tools.web_search] options.
type WebSearchBackendFactory func(opts WebSearchOptions) (WebSearchBackend, error)

var webSearchBackends = map[string]WebSearchBackendFactory{}

// RegisterWebSearchBackend adds a [tools.web_search] provider name. Intended
// for init(); it panics on a duplicate name, a compile-time wiring mistake.
func RegisterWebSearchBackend(name string, f WebSearchBackendFactory) {
	if _, dup := webSearchBackends[name]; dup {
		panic("builtin: duplicate web_search backend " + name)
	}
	webSearchBackends[name] = f
}

// WebSearchProviders returns the registered backend names, sorted.
func WebSearchProviders() []string {
	out := make([]string, 0, len(webSearchBackends))
	for name := range webSearchBackends {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// webSearch is the opt-in client-side web_search tool. Unlike web_fetch it is
// not registered at init: boot adds it only when [tools.web_search] names a
// backend, so a default config keeps today's tool set and any provider-side
// web_search untouched. It is reached through use_capability (tool:web_search),
// so it never shares the provider request schema with a provider-executed
// web_search tool. The tool owns argument limits, the timeout and formatting;
// the backend only runs the query.
type webSearch struct {
	provider string
	backend  WebSearchBackend
	timeout  time.Duration
}

const (
	webSearchTimeout        = 15 * time.Second
	webSearchDefaultResults = 5
	webSearchMaxResults     = 10
	webSearchSnippetChars   = 600
)

// ConfiguredWebSearch returns the client-side web_search tool for the
// [tools.web_search] provider, or nil when provider is empty (the default).
func ConfiguredWebSearch(provider string, opts WebSearchOptions) (tool.Tool, error) {
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" {
		return nil, nil
	}
	newBackend, ok := webSearchBackends[name]
	if !ok {
		return nil, fmt.Errorf("unknown web_search provider %q (supported: %s)", provider, strings.Join(WebSearchProviders(), ", "))
	}
	backend, err := newBackend(opts)
	if err != nil {
		return nil, fmt.Errorf("web_search provider %q: %w", name, err)
	}
	return webSearch{provider: name, backend: backend, timeout: webSearchTimeout}, nil
}

func (webSearch) Name() string { return "web_search" }

func (webSearch) Description() string {
	return "Search the web and return ranked results (title, URL, text excerpt) from the client-side backend configured in [tools.web_search]. Use it to find pages when you do not know the URL yet, then read a page in full with web_fetch. If the model already has provider-side web search, prefer that."
}

func (webSearch) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "query":{"type":"string","description":"Search query"},
  "max_results":{"type":"integer","description":"Number of results, 1-10 (default 5)"}
},
"required":["query"]
}`)
}

func (webSearch) ReadOnly() bool { return true }

// SnipHint keeps the top-ranked results when stale output is shortened.
func (webSearch) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 80, Tail: 8, HeadChars: 10000, TailChars: 1000}
}

func (ws webSearch) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	query := strings.TrimSpace(p.Query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	n := p.MaxResults
	if n <= 0 {
		n = webSearchDefaultResults
	}
	if n > webSearchMaxResults {
		n = webSearchMaxResults
	}

	searchCtx, cancel := context.WithTimeout(ctx, ws.timeout)
	defer cancel()
	results, err := ws.backend.Search(searchCtx, query, n)
	if err != nil {
		if ctx.Err() == nil && errors.Is(searchCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("web_search (%s): no response within %s", ws.provider, ws.timeout)
		}
		return "", fmt.Errorf("web_search (%s): %w", ws.provider, err)
	}
	if len(results) > n {
		results = results[:n]
	}
	return formatWebSearch(query, results), nil
}

func formatWebSearch(query string, results []WebSearchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("no results for %q", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d results for %q\n", len(results), query)
	for i, r := range results {
		url := strings.TrimSpace(r.URL)
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = url
		}
		fmt.Fprintf(&b, "\n%d. %s\n   %s\n", i+1, title, url)
		if text := webSearchExcerpt(r.Text); text != "" {
			fmt.Fprintf(&b, "   %s\n", text)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// webSearchExcerpt collapses whitespace and caps the page text so ten results
// stay well under a typical file read.
func webSearchExcerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > webSearchSnippetChars {
		s = strings.TrimSpace(string(r[:webSearchSnippetChars])) + "…"
	}
	return s
}
