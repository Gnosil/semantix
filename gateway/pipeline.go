package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"semantix/kernel/cache"
	"semantix/kernel/inject"
	"semantix/kernel/usage"
)

// handleChat runs the full pipeline for POST /v1/chat/completions
// (design §3.3): auth is done in the router; here: normalize → L3 → L2 →
// forward upstream → passthrough/SSE → sidecar + usage.
func (g *Gateway) handleChat(w http.ResponseWriter, r *http.Request, body []byte) {
	req, err := parseChatRequest(body)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	up, ok := g.cfg.UpstreamFor(req.Model)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "model_not_found",
			fmt.Sprintf("model %q is not routed by this gateway", req.Model))
		return
	}
	query, _ := lastUserText(req.Messages)
	chash, err := contextHash(req.Messages)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	scope, err := g.resolveScope(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	sessionID := r.Header.Get("x-semantix-session")
	ctx := r.Context()
	q := cache.Query{UserInput: query, ContextHash: chash, Scope: scope, Model: req.Model}

	// L3: verified reuse — zero upstream calls (design §3.3 step 3). The
	// kernel gate enforces context/model isolation (fail closed) on top of
	// the dep-fingerprint chain.
	if !g.disabled {
		if res, lerr := g.decider.DecideL3(ctx, q); lerr == nil && res != nil && g.l3Eligible(res.SliceID) {
			g.recordUsage(usage.Event{
				SessionID: sessionID, TokensIn: int64(len(query)/4) + int64(len(res.Response)/4),
				TokensOut: 0, CacheHitToken: int64(len(res.Response) / 4),
				L3Reuse: true, At: g.now().Unix(),
			})
			g.replyFromCache(w, r, req, res)
			return
		}
	}

	// L2: inject reuse context, then forward (design §3.3 steps 4-5).
	var injectedTokens int64
	var sliceHits int
	inj, ierr := g.injector.Build(query)
	if ierr != nil {
		log.Printf("gateway: inject: %v", ierr) // never blocks the main path
	}
	if inj != nil {
		injectedTokens = int64(inj.Bytes / 4)
		sliceHits = len(inj.Slices)
	}
	body = g.rewriteOutgoing(body, req, up, inj)

	resp, ferr := g.forward(ctx, up, body)
	if ferr != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error",
			fmt.Sprintf("upstream request failed: %v", ferr))
		return
	}
	defer resp.Body.Close()

	if req.Stream {
		g.streamThrough(w, resp, sessionID, req, chash, query, injectedTokens, sliceHits)
		return
	}
	g.passthrough(w, resp, sessionID, req, chash, query, injectedTokens, sliceHits)
}

// l3Eligible applies the gateway-side TTL window on top of the kernel
// verification chain. A slice that disappeared from the store also fails
// closed.
func (g *Gateway) l3Eligible(id string) bool {
	s, err := g.store.Get(id)
	if err != nil {
		return false
	}
	return g.cacheFresh(s)
}

// rewriteOutgoing rewrites the outgoing body in place: the client model
// alias is mapped to the upstream model name, and (when an injection block
// was assembled) the block is appended to the first system message
// (byte-stable prefix tail, L1) or prepended as a new system message. All
// other request fields pass through untouched.
func (g *Gateway) rewriteOutgoing(body []byte, req *chatRequest, up UpstreamConfig, inj *inject.Injection) []byte {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return body
	}
	raw["model"] = up.UpstreamModel
	if inj != nil && inj.Text != "" {
		messages := append([]chatMessage(nil), req.Messages...)
		raw["messages"] = attachBlock(messages, inj.Text)
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}

// attachBlock appends block to the last string-content system message (the
// system-prompt tail, design §3.6), or prepends a system message when none
// exists. Earlier system messages stay untouched.
func attachBlock(messages []chatMessage, block string) []chatMessage {
	last := -1
	for i := range messages {
		if messages[i].Role == "system" {
			if _, ok := messages[i].Content.(string); ok {
				last = i
			}
		}
	}
	if last >= 0 {
		messages[last].Content = messages[last].Content.(string) + "\n\n" + block
		return messages
	}
	return append([]chatMessage{{Role: "system", Content: block}}, messages...)
}

// forward posts the (rewritten) body to the upstream OpenAI-compatible
// endpoint with the upstream API key. Transport errors are retried once
// (design §3.8: the gateway does a single best-effort retry; the body is
// fully buffered so a resend is always safe); HTTP status errors are not
// retried — they carry the upstream's own verdict.
func (g *Gateway) forward(ctx context.Context, up UpstreamConfig, body []byte) (*http.Response, error) {
	url := strings.TrimRight(up.BaseURL, "/") + "/chat/completions"
	do := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+up.APIKey)
		return g.client.Do(req)
	}
	resp, err := do()
	if err != nil {
		log.Printf("gateway: upstream %s: %v (retrying once)", up.Name, err)
		resp, err = do()
		if err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// passthrough relays a non-streaming upstream response, records usage and
// writes the session sidecar (request turns + assistant reply) — only for
// successful exchanges, so failed requests never enter the reuse library.
func (g *Gateway) passthrough(w http.ResponseWriter, resp *http.Response, sessionID string, req *chatRequest, ctxHash string, query string, injectedTokens int64, sliceHits int) {
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error",
			fmt.Sprintf("read upstream response: %v", err))
		return
	}
	if resp.StatusCode >= 400 {
		// upstream error: relay the OpenAI error envelope, nothing reusable
		writeAPIError(w, resp.StatusCode, "upstream_error",
			fmt.Sprintf("upstream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(out))))
		return
	}
	content := extractAssistantContent(out)
	g.recordSession(sessionID, ctxHash, req.Model, turns(req, content))

	for k, vs := range resp.Header {
		if isHopByHopHeader(k) {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("x-semantix-cache", "miss")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(out)

	g.recordUsage(usage.Event{
		SessionID:      sessionID,
		TokensIn:       int64(len(query)/4) + injectedTokens,
		TokensOut:      int64(len(out) / 4),
		InjectedTokens: injectedTokens,
		SliceHits:      sliceHits,
		At:             g.now().Unix(),
	})
}

// streamThrough relays a streaming upstream response chunk by chunk (SSE),
// preserving events verbatim (design §3.4: never reorder/rewrite the
// upstream stream). If the upstream ends without a [DONE] marker (abnormal
// disconnect), the gateway appends one so the client never hangs on an
// unterminated stream. Sidecar records the request turns only — parsing the
// assistant content out of the SSE chunks is deferred (documented debt).
func (g *Gateway) streamThrough(w http.ResponseWriter, resp *http.Response, sessionID string, req *chatRequest, ctxHash string, query string, injectedTokens int64, sliceHits int) {
	if resp.StatusCode >= 400 {
		out, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		writeAPIError(w, resp.StatusCode, "upstream_error",
			fmt.Sprintf("upstream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(out))))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("x-semantix-cache", "miss")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)

	// Scan lines rather than blind blocks: we must detect the [DONE]
	// marker to guarantee stream termination even when the upstream
	// disconnects early. Lines are still forwarded verbatim.
	// Issue #187 / GW7 (spec §3.4): when the upstream finishes without a
	// usage event, we synthesize one before [DONE] carrying the injection
	// stats, so streaming clients see the same cached_tokens accounting as
	// non-streaming paths.
	br := bufio.NewReader(resp.Body)
	sawDone := false
	sawUsage := false
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimSpace(line)
			switch {
			case bytes.HasPrefix(trimmed, []byte("data: [DONE]")):
				sawDone = true
			case bytes.HasPrefix(trimmed, []byte("data:")):
				if sseDataHasUsage(trimmed) {
					sawUsage = true
				}
			}
			if sawDone && !sawUsage {
				// Synthesize the usage block before the terminator.
				_, _ = w.Write(g.streamUsageBlock(req.Model, int64(len(query)/4), injectedTokens))
				if flusher != nil {
					flusher.Flush()
				}
			}
			_, _ = w.Write(line)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
	if !sawDone {
		// Abnormal termination: close the stream ourselves so the client
		// does not hang (design §3.4: [DONE] always terminates). No usage
		// block: the stream never completed normally.
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
	g.recordSession(sessionID, ctxHash, req.Model, turns(req, ""))
	g.recordUsage(usage.Event{
		SessionID:      sessionID,
		TokensIn:       int64(len(query)/4) + injectedTokens,
		InjectedTokens: injectedTokens,
		SliceHits:      sliceHits,
		At:             g.now().Unix(),
	})
}

// sseDataHasUsage reports whether an SSE data line's JSON payload carries a
// top-level "usage" object (OpenAI-compatible streaming usage block).
func sseDataHasUsage(line []byte) bool {
	payload := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("data:")))
	var obj struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(payload, &obj); err != nil {
		return false
	}
	return len(obj.Usage) > 0 && string(obj.Usage) != "null"
}

// streamUsageBlock builds the synthetic OpenAI-compatible streaming usage
// block injected before [DONE] when the upstream omitted usage (Issue #187).
// Token counts are byte estimates (len/4), stamped with estimator for
// downstream accounting; cached_tokens carries the L2 injection stats.
func (g *Gateway) streamUsageBlock(model string, promptTokens, cachedTokens int64) []byte {
	chunk := struct {
		ID      string          `json:"id"`
		Object  string          `json:"object"`
		Created int64           `json:"created"`
		Model   string          `json:"model"`
		Choices []choicePayload `json:"choices"`
		Usage   usagePayload    `json:"usage"`
	}{
		ID:      "chatcmpl-stream",
		Object:  "chat.completion.chunk",
		Created: g.now().Unix(),
		Model:   model,
		Choices: []choicePayload{},
		Usage: usagePayload{
			PromptTokens:     int(promptTokens),
			CompletionTokens: 0,
			TotalTokens:      int(promptTokens),
			PromptDetails:    struct{ CachedTokens int `json:"cached_tokens"` }{CachedTokens: int(cachedTokens)},
			Estimator:        "bytes/4",
		},
	}
	raw, _ := json.Marshal(chunk)
	return append([]byte("data: "), append(raw, '\n', '\n')...)
}

// isHopByHopHeader reports connection-scoped headers that must never be
// relayed end-to-end (RFC 9110 §7.6.1): they describe the upstream hop.
func isHopByHopHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}

// turns renders the request messages plus the assistant reply as the
// ingest.JSONLSource-compatible sidecar lines.
func turns(req *chatRequest, assistant string) []map[string]any {
	out := make([]map[string]any, 0, len(req.Messages)+1)
	for _, m := range req.Messages {
		out = append(out, map[string]any{"role": m.Role, "content": textParts(m.Content)})
	}
	if assistant != "" {
		out = append(out, map[string]any{"role": "assistant", "content": assistant})
	}
	return out
}

// extractAssistantContent pulls choices[0].message.content out of a
// non-streaming OpenAI response (best-effort; empty on parse failure).
func extractAssistantContent(body []byte) string {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}

// recordUsage appends one kernel/usage event (best-effort).
func (g *Gateway) recordUsage(e usage.Event) {
	if g.usageLog == nil {
		return
	}
	if err := g.usageLog.Append(e); err != nil {
		log.Printf("gateway: usage append: %v", err)
	}
}

// ---------------------------------------------------------------------------
// L3 hit responses (design §3.4)

// chatCompletion is the synthetic non-streaming response for an L3 hit.
type chatCompletion struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Choices []choicePayload `json:"choices"`
	Usage   usagePayload    `json:"usage"`
}

type choicePayload struct {
	Index        int           `json:"index"`
	Message      messagePayload `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type messagePayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	PromptDetails    struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	// Estimator marks gateway-synthesized token counts (Issue #187 / GW7):
	// the numbers are byte estimates (len/4), not real tokenizer counts.
	// Omitted when the usage block is relayed verbatim from upstream.
	Estimator string `json:"estimator,omitempty"`
}

// replyFromCache serves a verified L3 hit: a plain JSON response, or a
// reconstructed SSE stream for stream=true (design §3.4 streaming replay).
func (g *Gateway) replyFromCache(w http.ResponseWriter, r *http.Request, req *chatRequest, res *cache.L3Result) {
	w.Header().Set("x-semantix-cache", "hit")
	if req.Stream {
		g.replayStream(w, r, req, res)
		return
	}
	cached := len(res.Response) / 4
	query, _ := lastUserText(req.Messages)
	prompt := len(query) / 4
	body := chatCompletion{
		ID:      "chatcmpl-" + res.SliceID,
		Object:  "chat.completion",
		Created: g.now().Unix(),
		Model:   req.Model,
		Choices: []choicePayload{{
			Index:        0,
			Message:      messagePayload{Role: "assistant", Content: res.Response},
			FinishReason: "stop",
		}},
		Usage: usagePayload{
			PromptTokens:     prompt + cached,
			CompletionTokens: 0,
			TotalTokens:      prompt + cached,
			Estimator:        "bytes/4",
		},
	}
	body.Usage.PromptDetails.CachedTokens = cached
	raw, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// replayStream rebuilds an SSE stream from the cached response, chunking the
// content into deltas (design §3.4 streaming replay; MVP chunk size 256B).
func (g *Gateway) replayStream(w http.ResponseWriter, r *http.Request, req *chatRequest, res *cache.L3Result) {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	base := struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
	}{"chatcmpl-" + res.SliceID, "chat.completion.chunk", g.now().Unix(), req.Model}

	writeChunk := func(delta map[string]any, finish any) {
		evt := map[string]any{
			"id": base.ID, "object": base.Object, "created": base.Created, "model": base.Model,
			"choices": []map[string]any{{
				"index": 0, "delta": delta, "finish_reason": finish,
			}},
		}
		raw, _ := json.Marshal(evt)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		if flusher != nil {
			flusher.Flush()
		}
	}

	writeChunk(map[string]any{"role": "assistant"}, nil)
	content := res.Response
	for len(content) > 0 {
		n := 256
		if len(content) < n {
			n = len(content)
		}
		writeChunk(map[string]any{"content": content[:n]}, nil)
		content = content[n:]
	}
	writeChunk(map[string]any{}, "stop")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
