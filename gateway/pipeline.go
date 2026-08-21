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
	// Route this turn's grey-zone judge decisions back to this turn's usage
	// event (Issue #242 gap 1). The decider's OnJudge hook is process-wide,
	// so the request context is what identifies the caller.
	jc := &judgeCollector{}
	ctx := withJudgeCollector(r.Context(), jc)
	q := cache.Query{
		UserInput: query, ContextHash: chash, Scope: scope, Model: req.Model,
		Freshness: cache.Freshness{NowUnix: g.now().Unix(), TTLSeconds: g.cfg.TTLFor(up.Vendor)},
	}

	// L3: verified reuse — zero upstream calls (design §3.3 step 3). The
	// kernel gate enforces context/model isolation (fail closed) on top of
	// the dep-fingerprint chain. TTL is vendor-aware (design §3.5).
	if !g.disabled {
		if res, lerr := g.decider.DecideL3(ctx, q); lerr == nil && res != nil && g.l3Eligible(res.SliceID, up.Vendor) {
			g.recordUsage(usage.Event{
				SessionID: sessionID, TokensIn: int64(len(query)/4) + int64(len(res.Response)/4),
				TokensOut: 0, CacheHitToken: int64(len(res.Response) / 4),
				L3Reuse: true, JudgeDecisions: jc.drain(), At: g.now().Unix(),
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
	if up.Vendor == "anthropic" {
		// Anthropic hop (design §0.5): translate the OpenAI body to the
		// /v1/messages shape, applying the L2 injection block with
		// cache_control breakpoints. OpenAI passthrough is untouched.
		abody, aerr := toAnthropicRequest(body, up, inj)
		if aerr != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error",
				"anthropic conversion: "+aerr.Error())
			return
		}
		body = abody
	} else {
		body = g.rewriteOutgoing(body, req, up, inj)
	}

	resp, ferr := g.forward(ctx, up, body)
	if ferr != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error",
			fmt.Sprintf("upstream request failed: %v", ferr))
		return
	}
	defer resp.Body.Close()

	if req.Stream {
		if up.Vendor == "anthropic" {
			g.streamThroughAnthropic(w, resp, sessionID, req, chash, query, injectedTokens, sliceHits, jc)
			return
		}
		g.streamThrough(w, resp, sessionID, req, chash, query, injectedTokens, sliceHits, jc)
		return
	}
	g.passthrough(w, resp, sessionID, req, chash, query, injectedTokens, sliceHits, up.Vendor, jc)
}

// l3Eligible applies the gateway-side TTL window on top of the kernel
// verification chain. A slice that disappeared from the store also fails
// closed.
func (g *Gateway) l3Eligible(id, vendor string) bool {
	s, err := g.store.Get(id)
	if err != nil || s == nil {
		// s == nil: the candidate slice left the store (concurrent
		// eviction/compact, or an index entry whose persistence was rolled
		// back) after DecideL3 retrieved it. Fail closed rather than
		// dereference nil in cacheFresh.
		return false
	}
	return g.cacheFresh(s, vendor)
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

// forward posts the (rewritten) body to the upstream endpoint with the
// upstream API key. vendor="anthropic" hits POST {base}/v1/messages with the
// x-api-key + anthropic-version headers (design §0.5); every other vendor
// hits /chat/completions with Authorization: Bearer. Transport errors are
// retried once (design §3.8: the gateway does a single best-effort retry;
// the body is fully buffered so a resend is always safe); HTTP status errors
// are not retried — they carry the upstream's own verdict.
func (g *Gateway) forward(ctx context.Context, up UpstreamConfig, body []byte) (*http.Response, error) {
	path := "/chat/completions"
	if up.Vendor == "anthropic" {
		path = "/messages"
	}
	url := strings.TrimRight(up.BaseURL, "/") + path
	do := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if up.Vendor == "anthropic" {
			req.Header.Set("x-api-key", up.APIKey)
			req.Header.Set("anthropic-version", anthropicVersion)
		} else {
			req.Header.Set("Authorization", "Bearer "+up.APIKey)
		}
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
// Anthropic responses are translated to OpenAI chat.completion shape first
// (the client surface is always OpenAI-compatible).
func (g *Gateway) passthrough(w http.ResponseWriter, resp *http.Response, sessionID string, req *chatRequest, ctxHash string, query string, injectedTokens int64, sliceHits int, vendor string, jc *judgeCollector) {
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error",
			fmt.Sprintf("read upstream response: %v", err))
		return
	}
	if resp.StatusCode >= 400 {
		// upstream error: relay the OpenAI error envelope, nothing reusable
		msg := strings.TrimSpace(string(out))
		if vendor == "anthropic" {
			msg = anthropicError(out) // Anthropic errors use {error:{message}}
		}
		writeAPIError(w, resp.StatusCode, "upstream_error",
			fmt.Sprintf("upstream returned %d: %s", resp.StatusCode, msg))
		return
	}
	if vendor == "anthropic" {
		out, err = anthropicToOpenAIResponse(out, req.Model)
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "upstream_error",
				fmt.Sprintf("translate anthropic response: %v", err))
			return
		}
	}
	content := extractAssistantContent(out)
	g.recordSession(sessionID, ctxHash, req.Model, turns(req, content))

	for k, vs := range resp.Header {
		if isHopByHopHeader(k) {
			continue
		}
		if vendor == "anthropic" && http.CanonicalHeaderKey(k) == "Content-Length" {
			// Only the translated hop may differ from the upstream body
			// length; net/http recomputes Content-Length, so a copied value
			// would truncate or stall the reply. The OpenAI passthrough path
			// relays verbatim and keeps the upstream framing byte-identical.
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
		JudgeDecisions: jc.drain(),
		At:             g.now().Unix(),
	})
}

// streamThrough relays a streaming upstream response chunk by chunk (SSE),
// preserving events verbatim (design §3.4: never reorder/rewrite the
// upstream stream). While relaying, the assistant content is aggregated out
// of the SSE chunks (choices[0].delta.content); only a cleanly terminated
// stream — finish_reason or [DONE], within the aggregation bound — is
// written to the session sidecar. Partial/aborted streams fail closed and
// never enter the reuse library, matching the non-streaming path where
// failed exchanges are not recorded (design §0.3 documented debt, now paid).
// If the upstream ends without a [DONE] marker (abnormal disconnect), the
// gateway appends one so the client never hangs on an unterminated stream.
func (g *Gateway) streamThrough(w http.ResponseWriter, resp *http.Response, sessionID string, req *chatRequest, ctxHash string, query string, injectedTokens int64, sliceHits int, jc *judgeCollector) {
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
	// disconnects early. Lines are still forwarded verbatim, and fed to
	// the aggregator for sidecar extraction.
	agg := newSSEAggregator(maxSSEAggregateBytes)
	br := bufio.NewReader(resp.Body)
	sawDone := false
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			// Aggregate before relaying so a [DONE] line flips the
			// completion state in time: the synthetic usage chunk must
			// land before [DONE], not after it (design §3.4).
			agg.Feed(line)
			if bytes.HasPrefix(bytes.TrimSpace(line), []byte("data: [DONE]")) {
				sawDone = true
				if !agg.SawUsage() && !agg.Overflowed() {
					// Upstream sent no usage accounting: synthesize one
					// carrying the injection statistics (byte/4 flag).
					// [DONE] itself is the termination signal here — the
					// aggregator's done flag only flips on the trailing
					// blank line, so trust sawDone, not Complete().
					prompt := int64(len(query)/4) + injectedTokens
					g.writeUsageChunk(w, flusher, req, agg.EventID(), prompt, int64(len(agg.Content())/4), injectedTokens)
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
		// does not hang (design §3.4: [DONE] always terminates). Half
		// streams never get a usage chunk — their meters are unreliable,
		// fail closed.
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
	if agg.Complete() {
		g.recordSession(sessionID, ctxHash, req.Model, turns(req, agg.Content()))
	}
	tokensOut := int64(0)
	if agg.Complete() {
		tokensOut = int64(len(agg.Content()) / 4)
	}
	g.recordUsage(usage.Event{
		SessionID:      sessionID,
		TokensIn:       int64(len(query)/4) + injectedTokens,
		TokensOut:      tokensOut,
		InjectedTokens: injectedTokens,
		SliceHits:      sliceHits,
		JudgeDecisions: jc.drain(),
		At:             g.now().Unix(),
	})
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
	Index        int            `json:"index"`
	Message      messagePayload `json:"message"`
	FinishReason string         `json:"finish_reason"`
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
	// Estimator marks synthetic usage whose token counts are byte/4
	// estimates rather than tokenizer counts — billing reconciliation
	// must be able to tell them apart (design §0.3). Never set on usage
	// relayed verbatim from an upstream.
	Estimator string `json:"estimator,omitempty"`
}

// writeUsageChunk emits an OpenAI stream-usage event (empty choices +
// usage) just before [DONE], used when the upstream stream carried no
// usage accounting (design §3.4). Token counts are byte/4 estimates,
// flagged with "estimator":"bytes/4" so reconciliation can identify them.
// id reuses the stream's first chunk id (or falls back to a fresh one) so
// clients tracking the stream by id never see a foreign id.
func (g *Gateway) writeUsageChunk(w http.ResponseWriter, flusher http.Flusher, req *chatRequest, id string, prompt, completion, cached int64) {
	if id == "" {
		id = "chatcmpl-" + randomID()
	}
	evt := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": g.now().Unix(),
		"model":   req.Model,
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
			"total_tokens":      prompt + completion,
			"prompt_tokens_details": map[string]any{
				"cached_tokens": cached,
			},
			"estimator": "bytes/4",
		},
	}
	raw, _ := json.Marshal(evt)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	if flusher != nil {
		flusher.Flush()
	}
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
	// Synthetic usage for the replayed stream: a streamed L3 hit must
	// still carry billable usage (design §4.3), in the same byte/4
	// estimate shape as the non-streaming synthetic response.
	query, _ := lastUserText(req.Messages)
	cached := int64(len(res.Response) / 4)
	g.writeUsageChunk(w, flusher, req, base.ID, int64(len(query)/4)+cached, 0, cached)
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
