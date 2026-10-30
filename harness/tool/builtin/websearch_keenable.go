package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"semantix/harness/netclient"
)

func init() { RegisterWebSearchBackend("keenable", newKeenableSearch) }

// keenableSearch is the Keenable backend for web_search ([tools.web_search]
// provider = "keenable"). Without a key it calls the keyless endpoint, which
// is rate limited per IP; a key only switches to the keyed endpoint, which has
// higher limits.
type keenableSearch struct {
	baseURL   string
	apiKeyEnv string
	client    *http.Client
}

const (
	keenableBaseURL       = "https://api.keenable.ai"
	keenableDefaultKeyEnv = "KEENABLE_API_KEY"
	keenableAppTitle      = "Semantix" // X-Keenable-Title, required on the keyless endpoint
	keenableMaxRead       = 1 << 20
	keenableMaxErrorBody  = 300
)

func newKeenableSearch(opts WebSearchOptions) (WebSearchBackend, error) {
	client, err := netclient.NewHTTPClient(opts.Proxy, netclient.TransportOptions{})
	if err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}
	// Never follow redirects: a 307/308 would replay the query and X-API-Key
	// to whatever origin the Location names. ErrUseLastResponse hands the 3xx
	// to the non-200 handling below without issuing the redirected request.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	env := strings.TrimSpace(opts.APIKeyEnv)
	if env == "" {
		env = keenableDefaultKeyEnv
	}
	return keenableSearch{baseURL: keenableBaseURL, apiKeyEnv: env, client: client}, nil
}

func (k keenableSearch) Search(ctx context.Context, query string, maxResults int) ([]WebSearchResult, error) {
	body, err := json.Marshal(map[string]any{"query": query, "max_results": maxResults})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	// The key is read per call, so one set in Semantix's .env or the
	// environment applies without a config change.
	endpoint := k.baseURL + "/v1/search/public"
	key := strings.TrimSpace(os.Getenv(k.apiKeyEnv))
	if key != "" {
		endpoint = k.baseURL + "/v1/search"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Keenable-Title", keenableAppTitle)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, keenableMaxRead))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > keenableMaxErrorBody {
			msg = strings.ToValidUTF8(msg[:keenableMaxErrorBody], "")
		}
		if resp.StatusCode == http.StatusTooManyRequests && key == "" {
			msg += fmt.Sprintf(" (keyless rate limit; set %s for higher limits)", k.apiKeyEnv)
		}
		return nil, fmt.Errorf("status %s: %s", resp.Status, msg)
	}

	// description is not populated by this API; the page text is in snippet.
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	results := make([]WebSearchResult, 0, len(out.Results))
	for _, r := range out.Results {
		results = append(results, WebSearchResult{Title: r.Title, URL: r.URL, Text: r.Snippet})
	}
	return results, nil
}
