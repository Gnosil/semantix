package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ModelEmbedderConfig wires a remote OpenAI-compatible embeddings API
// (Issue #63, option A). The API key is read from the environment
// (SEMANTIX_EMBED_API_KEY), never stored in config or code. Fallback
// defaults to HashEmbedder{} so a remote failure degrades to the
// zero-dependency hash vectors instead of failing the caller (fail-soft).
type ModelEmbedderConfig struct {
	BaseURL  string // e.g. https://api.openai.com/v1
	Model    string // e.g. text-embedding-3-small
	APIKey   string
	Timeout  time.Duration // 0 -> 30s
	Dim      int           // expected vector dimension; 0 disables the check
	Fallback Embedder      // nil -> HashEmbedder{}
	// OnFallback, when set, is called once per degraded batch with the
	// error that caused the fallback (used by the CLI to warn on stderr).
	OnFallback func(error)
}

// ModelEmbedder implements Embedder over a remote embeddings endpoint.
type ModelEmbedder struct {
	cfg ModelEmbedderConfig
	hc  *http.Client
}

// NewModelEmbedder validates the config and builds the embedder.
func NewModelEmbedder(cfg ModelEmbedderConfig) (*ModelEmbedder, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return nil, fmt.Errorf("embed: base-url and model are required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("embed: API key is required (set SEMANTIX_EMBED_API_KEY)")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Fallback == nil {
		cfg.Fallback = HashEmbedder{}
	}
	return &ModelEmbedder{cfg: cfg, hc: &http.Client{Timeout: cfg.Timeout}}, nil
}

// Model returns the remote model name, so callers can record which model
// produced a vector.
func (m *ModelEmbedder) Model() string {
	return m.cfg.Model
}

// Embed asks the remote endpoint for vectors and L2-normalizes them (the
// VectorIndex contract: cosine == dot product). Any remote failure — HTTP
// status, transport error, timeout, malformed body, or dimension mismatch —
// degrades the whole batch to the configured fallback (fail-soft); an error
// is returned only when the fallback itself fails.
func (m *ModelEmbedder) Embed(texts []string) ([][]float32, error) {
	vecs, _, err := m.EmbedReport(texts)
	return vecs, err
}

// EmbedReport is Embed plus whether the batch was served by the fallback
// embedder, so callers can record the true producer of each vector.
func (m *ModelEmbedder) EmbedReport(texts []string) ([][]float32, bool, error) {
	if len(texts) == 0 {
		return [][]float32{}, false, nil
	}
	vecs, err := m.call(context.Background(), texts)
	if err != nil {
		if m.cfg.OnFallback != nil {
			m.cfg.OnFallback(err)
		}
		fallbackVecs, fallbackErr := m.cfg.Fallback.Embed(texts)
		return fallbackVecs, true, fallbackErr
	}
	return vecs, false, nil
}

func (m *ModelEmbedder) call(ctx context.Context, texts []string) ([][]float32, error) {
	body := map[string]any{
		"model": m.cfg.Model,
		"input": texts,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("embed: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.BaseURL+"/embeddings", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("embed: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.cfg.APIKey)
	resp, err := m.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: call: %w", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("embed: read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Status only — never echo the response body: a user-configured
		// endpoint/proxy could reflect the Authorization header into it.
		return nil, fmt.Errorf("embed: %s: HTTP %d (use https endpoints)", m.cfg.BaseURL+"/embeddings", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("embed: response: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embed: response has %d vectors, want %d", len(parsed.Data), len(texts))
	}
	if m.cfg.Dim > 0 {
		for i, d := range parsed.Data {
			if len(d.Embedding) != m.cfg.Dim {
				return nil, fmt.Errorf("embed: vector %d has dim %d, want %d", i, len(d.Embedding), m.cfg.Dim)
			}
		}
	}
	vecs := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		vec := append([]float32(nil), d.Embedding...)
		normalize(vec)
		vecs[i] = vec
	}
	return vecs, nil
}
