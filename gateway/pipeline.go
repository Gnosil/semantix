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
	"time"
	"unicode/utf8"

	"semantix/kernel/cache"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/inject"
	"semantix/kernel/slice"
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

	// L3 negative-observability carry (Issue #262): the per-turn rejection
	// detail from the L3 decision, folded into the downstream usage record.
	var l3Obs cache.Obs

	// L3: verified reuse — zero upstream calls (design §3.3 step 3). The
	// kernel gate enforces context/model isolation (fail closed) on top of
	// the dep-fingerprint chain. TTL is vendor-aware (design §3.5).
	l3FalseHit := false
	if !g.disabled {
		// Suspected false hit (Issue #262 §3.3): the user retries a query
		// that was just served from L3 in this session — bypass L3 and go
		// upstream so the retry gets a fresh answer, and record the
		// negative signal in the usage event.
		if g.detectFalseHit(sessionID, query) {
			l3FalseHit = true
		} else {
			res, obs := g.decideL3(ctx, q)
			if res != nil && g.l3Eligible(res.SliceID, up.Vendor) {
				now := g.now()
				g.recordUsage(g.withL3Obs(usage.Event{
					SessionID: sessionID, Provider: up.Name, Vendor: up.Vendor, Model: req.Model,
					TokensIn:  int64(len(query)/4) + int64(len(res.Response)/4),
					TokensOut: 0, CacheHitToken: int64(len(res.Response) / 4),
					L3Reuse: true, L3SliceID: res.SliceID, JudgeDecisions: jc.drain(), At: now.Unix(),
				}, obs, false))
				g.recordSliceStats(map[string]slice.SliceStats{
					res.SliceID: {Hits: 1, LastUsed: now.Unix()},
				})
				g.recordSliceEvent(sessionID, chash, req.Model, kernelevent.SliceHit,
					kernelevent.SliceHitPayload{Layer: "L3", SliceIDs: []string{res.SliceID}}, now)
				g.recordL3Reuse(sessionID, query, res.SliceID)
				g.replyFromCache(w, r, req, res)
				return
			}
			// L3 did not serve this request; carry the rejection detail
			// into the turn's usage record (all-zero → omitted by the
			// additive omitempty fields).
			l3Obs = obs
		}
	}

	// L2: inject reuse context, then forward (design §3.3 steps 4-5).
	var injectedTokens int64
	var sliceHits int
	inj, ierr := g.injector.Build(query)
	if ierr != nil {
		log.Printf("gateway: inject: %v", ierr) // never blocks the main path
	}
	g.recordRetrieval(sessionID, query, inj, g.now())
	if inj != nil {
		injectedTokens = int64(inj.Bytes / 4)
		sliceHits = len(inj.Slices)
		if len(inj.Slices) > 0 {
			now := g.now()
			ids := make([]string, 0, len(inj.Slices))
			deltas := make(map[string]slice.SliceStats, len(inj.Slices))
			for _, sl := range inj.Slices {
				if sl == nil {
					continue
				}
				ids = append(ids, sl.ID)
				deltas[sl.ID] = slice.SliceStats{Injected: 1, LastUsed: now.Unix()}
			}
			if len(ids) > 0 {
				g.recordSliceStats(deltas)
				g.recordSliceEvent(sessionID, chash, req.Model, kernelevent.SliceInject,
					kernelevent.SliceInjectPayload{SliceIDs: ids, Bytes: inj.Bytes}, now)
			}
		}
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
			g.streamThroughAnthropic(w, resp, sessionID, req, chash, query, injectedTokens, sliceHits, up, jc, l3Obs, l3FalseHit)
			return
		}
		g.streamThrough(w, resp, sessionID, req, chash, query, injectedTokens, sliceHits, up, jc, l3Obs, l3FalseHit)
		return
	}
	g.passthrough(w, resp, sessionID, req, chash, query, injectedTokens, sliceHits, up, jc, l3Obs, l3FalseHit)
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
// alias is mapped to the upstream model name, the prefix-hygiene middleware
// runs (GLM-P0-2, #290: attribution stripping / tools canonicalization /
// per-upstream cache_control policy), and (when an injection block was
// assembled) the block is appended to the first system message (byte-stable
// prefix tail, L1) or prepended as a new system message. All other request
// fields pass through untouched. Sanitation operates on the decoded body
// before injection so both the injected and non-injected paths ship the
// same sanitized prefix.
func (g *Gateway) rewriteOutgoing(body []byte, req *chatRequest, up UpstreamConfig, inj *inject.Injection) []byte {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return body
	}
	raw["model"] = up.UpstreamModel
	g.sanitizeOutgoing(raw, up)
	// OpenAI-style path forwards reasoning_effort verbatim; surface a
	// vocabulary drift in the log instead of an unattributable upstream 400.
	if effort, _ := raw["reasoning_effort"].(string); effort != "" {
		validateEffortForOpenAIPath(effort)
	}
	if inj != nil && inj.Text != "" {
		if msgs, ok := raw["messages"].([]any); ok {
			raw["messages"] = attachBlockRaw(msgs, inj.Text)
		}
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}

// attachBlockRaw is attachBlock over the decoded JSON form: it appends block
// to the last string-content system message, or prepends a system message
// when none exists. Operating on the decoded body (rather than re-encoding
// req.Messages) preserves whatever the sanitizer just did to the prefix.
func attachBlockRaw(messages []any, block string) []any {
	last := -1
	for i, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok || msg["role"] != "system" {
			continue
		}
		if _, ok := msg["content"].(string); ok {
			last = i
		}
	}
	if last >= 0 {
		msg := messages[last].(map[string]any)
		msg["content"] = msg["content"].(string) + "\n\n" + block
		return messages
	}
	return append([]any{map[string]any{"role": "system", "content": block}}, messages...)
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
func (g *Gateway) passthrough(w http.ResponseWriter, resp *http.Response, sessionID string, req *chatRequest, ctxHash string, query string, injectedTokens int64, sliceHits int, up UpstreamConfig, jc *judgeCollector, l3Obs cache.Obs, l3FalseHit bool) {
	vendor := up.Vendor
	// Read one byte past the cap so a truncation is detectable: io.LimitReader
	// stops silently at the limit and io.ReadAll reports no error, which would
	// otherwise let a >maxBodyBytes upstream body be recorded into the reuse
	// library and served with a mismatched Content-Length (Issue #368).
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error",
			fmt.Sprintf("read upstream response: %v", err))
		return
	}
	if len(out) > maxBodyBytes {
		writeAPIError(w, http.StatusBadGateway, "upstream_error",
			fmt.Sprintf("upstream response exceeds %d bytes", maxBodyBytes))
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
		if http.CanonicalHeaderKey(k) == "Content-Length" {
			// Never forward the upstream Content-Length: net/http recomputes it
			// from the bytes we actually write, so a copied value would stall or
			// malform the reply whenever our body differs from upstream's — the
			// Anthropic→OpenAI translation changes length, and any vendor's body
			// could have been size-capped on read (Issue #368).
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("x-semantix-cache", "miss")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(out)

	// Real provider accounting when the upstream carried usage (GLM-P0-3 /
	// #291); the bytes/4 estimate stays as the fallback, marked Exact=false.
	ev := g.withL3Obs(usage.Event{
		Provider:       up.Name,
		Vendor:         vendor,
		Model:          req.Model,
		TokensIn:       int64(len(query)/4) + injectedTokens,
		TokensOut:      int64(len(out) / 4),
		InjectedTokens: injectedTokens,
		SliceHits:      sliceHits,
		JudgeDecisions: jc.drain(),
		At:             g.now().Unix(),
	}, l3Obs, l3FalseHit)
	if nu, ok := parseUsageBody(out); ok {
		ev.TokensIn = nu.Prompt
		ev.TokensOut = nu.Completion
		ev.CacheHitToken = nu.CacheHit
		ev.Exact = true
	}
	g.recordUsage(ev)
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
func (g *Gateway) streamThrough(w http.ResponseWriter, resp *http.Response, sessionID string, req *chatRequest, ctxHash string, query string, injectedTokens int64, sliceHits int, up UpstreamConfig, jc *judgeCollector, l3Obs cache.Obs, l3FalseHit bool) {
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
	ev := g.withL3Obs(usage.Event{
		SessionID:      sessionID,
		Provider:       up.Name,
		Vendor:         up.Vendor,
		Model:          req.Model,
		TokensIn:       int64(len(query)/4) + injectedTokens,
		TokensOut:      tokensOut,
		InjectedTokens: injectedTokens,
		SliceHits:      sliceHits,
		JudgeDecisions: jc.drain(),
		At:             g.now().Unix(),
	}, l3Obs, l3FalseHit)
	// The relayed stream's own usage frame is the real provider accounting
	// (GLM-P0-3 / #291): prefer it over the bytes/4 estimate. writeUsageChunk
	// syntheses never land here — the aggregator only captured upstream frames.
	if nu, ok := parseUsageRaw(agg.UsageRaw()); ok {
		ev.TokensIn = nu.Prompt
		ev.TokensOut = nu.Completion
		ev.CacheHitToken = nu.CacheHit
		ev.Exact = true
	}
	g.recordUsage(ev)
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

// recordUsage appends one kernel/usage event (best-effort) and meters the
// free-tier quota. Every reply path funnels through here exactly once, so
// this is the single billing tap: forwarded requests consume the tier,
// L3 verified-reuse hits cost the platform nothing and stay free.
func (g *Gateway) recordUsage(e usage.Event) {
	if g.quota != nil && !e.L3Reuse {
		if err := g.quota.Consume(e.TokensIn + e.TokensOut); err != nil {
			log.Printf("gateway: quota persist: %v", err)
		}
	}
	if g.usageLog == nil {
		return
	}
	if err := g.usageLog.Append(e); err != nil {
		log.Printf("gateway: usage append: %v", err)
	}
}

// ---------------------------------------------------------------------------
// L3 negative observability (Issue #262)

// decideL3 runs the shared decider on a per-request copy so the Obs
// accumulator stays request-local: concurrent requests never mix their
// rejection counters, and the returned snapshot is this call's exact delta.
// A decider error behaves like a miss (fail-closed) — the snapshot still
// carries whatever the decision path counted.
func (g *Gateway) decideL3(ctx context.Context, q cache.Query) (*cache.L3Result, cache.Obs) {
	d := *g.decider
	acc := &cache.ObsAccum{}
	d.Obs = acc
	res, err := d.DecideL3(ctx, q)
	if err != nil {
		log.Printf("gateway: L3 decide: %v", err)
		return nil, acc.Snapshot()
	}
	return res, acc.Snapshot()
}

// falseHitSim resolves the retry-detection threshold: -1 (disabled) → 0,
// 0/unspecified → DefaultFalseHitSim, otherwise the configured value.
func (g *Gateway) falseHitSim() float64 {
	switch {
	case g.cfg.Cache.FalseHitSim < 0:
		return 0 // explicitly disabled
	case g.cfg.Cache.FalseHitSim == 0:
		return DefaultFalseHitSim
	default:
		return g.cfg.Cache.FalseHitSim
	}
}

// detectFalseHit reports whether this request retries the query that was
// just served from L3 in the same session (Issue #262 §3.3 heuristic), and
// consumes the record so one retry counts once. No session header, a
// disabled threshold, or no prior reuse → false.
func (g *Gateway) detectFalseHit(sessionID, query string) bool {
	sim := g.falseHitSim()
	if sessionID == "" || sim <= 0 {
		return false
	}
	g.reuseMu.Lock()
	defer g.reuseMu.Unlock()
	entry, ok := g.l3Reuses[sessionID]
	if !ok {
		return false
	}
	delete(g.l3Reuses, sessionID)
	if normalizedEditRatio(entry.Query, query) >= sim {
		// Suspected false hit (Issue #262 §3.3): the user retried a query
		// the cache just served. Feed negative evidence to the per-entry
		// adaptive engine (Issue #259 阶段 3) so the offending slice's
		// threshold tightens.
		if g.adapt != nil && entry.SliceID != "" {
			g.adapt.Observe(entry.SliceID, true, g.zones.TauLow)
		}
		return true
	}
	return false
}

// recordL3Reuse remembers the most recent L3-served request per session
// (bounded: maxL3ReuseEntries, LRU eviction by timestamp) and feeds the
// positive evidence to the per-entry adaptive engine (Issue #259 阶段 3):
// the served slice just earned a clean reuse.
func (g *Gateway) recordL3Reuse(sessionID, query, sliceID string) {
	if g.adapt != nil && sliceID != "" {
		g.adapt.Observe(sliceID, false, g.zones.TauLow)
	}
	if sessionID == "" {
		return
	}
	g.reuseMu.Lock()
	defer g.reuseMu.Unlock()
	g.l3Reuses[sessionID] = l3ReuseEntry{Query: query, SliceID: sliceID, At: g.now()}
	if len(g.l3Reuses) > maxL3ReuseEntries {
		var oldestKey string
		var oldest time.Time
		for k, v := range g.l3Reuses {
			if oldestKey == "" || v.At.Before(oldest) {
				oldestKey, oldest = k, v.At
			}
		}
		delete(g.l3Reuses, oldestKey)
	}
}

// withL3Obs folds the per-turn L3 decision detail into a usage event
// (Issue #262). All fields are additive; zero values are omitted by the
// event's omitempty tags.
func (g *Gateway) withL3Obs(e usage.Event, o cache.Obs, falseHit bool) usage.Event {
	e.L3GreyCandidates = o.Grey
	e.L3JudgeReject = o.JudgeReject
	e.L3JudgeError = o.JudgeError
	e.L3JudgeApproved = o.JudgeApproved
	e.L3RulesReject = o.RulesReject
	e.L3FingerprintReject = o.FingerprintReject
	e.L3IsolatedReject = o.IsolatedReject
	e.L3FalseHit = falseHit
	return e
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

// recordSliceStats applies one request's per-slice deltas off the HTTP hot
// path. Close waits for these tasks through the same lifecycle gate used by
// asynchronous session ingestion, so accepted observations are not dropped.
func (g *Gateway) recordSliceStats(deltas map[string]slice.SliceStats) {
	if len(deltas) == 0 {
		return
	}
	g.shutdownMu.Lock()
	if g.closing {
		g.shutdownMu.Unlock()
		return
	}
	g.ingestWG.Add(1)
	g.shutdownMu.Unlock()
	go func() {
		defer g.ingestWG.Done()
		if err := slice.ApplyStats(g.store, deltas); err != nil {
			log.Printf("gateway: slice stats: %v", err)
		}
	}()
}

func (g *Gateway) recordSliceEvent(sessionID, ctxHash, model string, kind kernelevent.Kind, payload any, at time.Time) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	g.recordKernelEvent(sessionID, ctxHash, model, kernelevent.Event{
		Kind: kind, SessionID: sessionID, At: at.UTC(), Data: data,
	})
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
		if n >= len(content) {
			n = len(content)
		} else {
			// Never split a multi-byte UTF-8 rune across chunks: json.Marshal
			// would replace the torn bytes with U+FFFD, corrupting the replayed
			// text at 256-byte boundaries (Issue #369). Back off to a rune
			// start; the max UTF-8 rune is 4 bytes, so n stays >= 253.
			for n > 0 && !utf8.RuneStart(content[n]) {
				n--
			}
			if n == 0 { // unreachable (no rune > 256B); flush the rest to be safe
				n = len(content)
			}
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
