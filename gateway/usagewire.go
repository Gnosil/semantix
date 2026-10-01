package gateway

import "encoding/json"

// upstreamUsage is the union of the cache-usage wire shapes observed across
// upstreams (GLM-P0-3 / Issue #291; measured in docs/reports/glm-spike-week.md
// §2, §3A.2, §3B):
//
//   - OpenAI style: prompt_tokens is the full input;
//     prompt_tokens_details.cached_tokens is the prefix-cache-served subset.
//   - Anthropic style: input_tokens EXCLUDES cache reads and writes
//     (incremental accounting) — the full input is
//     input_tokens + cache_creation_input_tokens + cache_read_input_tokens.
//     Tencent-style gateways emit cache_read_input_tokens only on a hit;
//     AtomClub-style gateways always carry the cache field family.
//   - DeepSeek style: top-level prompt_cache_{hit,miss}_tokens.
type upstreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// DeepSeek
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	// Anthropic style
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	// OpenAI style
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
		// CacheCreationTokens is a semantix extension on the OpenAI shape:
		// the gateway's Anthropic adapter carries the upstream
		// cache_creation_input_tokens here so OpenAI-surface clients (and
		// the gateway's own usage log) can see cache writes.
		CacheCreationTokens int `json:"cache_creation_tokens"`
	} `json:"prompt_tokens_details"`
	// Estimator marks usage synthesized by a semantix gateway (bytes/4).
	// It must never be re-ingested as exact provider accounting.
	Estimator string `json:"estimator"`
}

// normUsage is provider usage normalized to one accounting convention:
// Prompt is the complete input (cache hits included), CacheHit the subset
// served from the provider prefix cache, CacheWrite the subset written into
// it (billed at the cache-write premium; 0 when the provider reports none
// or has no write premium), Completion the full output.
type normUsage struct {
	Prompt     int64
	Completion int64
	CacheHit   int64
	CacheWrite int64
}

// normalizeUpstreamUsage folds one wire usage into normUsage. ok is false
// when the object carries no token accounting at all, or is a semantix
// bytes/4 synthesis (Estimator set).
func normalizeUpstreamUsage(u *upstreamUsage) (normUsage, bool) {
	if u == nil || u.Estimator != "" {
		return normUsage{}, false
	}
	prompt := u.PromptTokens
	if prompt == 0 && (u.InputTokens != 0 || u.CacheCreationInputTokens != 0 || u.CacheReadInputTokens != 0) {
		// Anthropic-style incremental input: reconcile to the full prompt
		// (measured: cache_read + input = full input, glm-spike-week.md §2).
		prompt = u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	}
	completion := u.CompletionTokens
	if completion == 0 {
		completion = u.OutputTokens
	}
	hit := u.PromptCacheHitTokens
	if hit == 0 && u.PromptTokensDetails != nil {
		hit = u.PromptTokensDetails.CachedTokens
	}
	if hit == 0 {
		hit = u.CacheReadInputTokens
	}
	write := u.CacheCreationInputTokens
	if write == 0 && u.PromptTokensDetails != nil {
		write = u.PromptTokensDetails.CacheCreationTokens
	}
	if prompt == 0 && completion == 0 && hit == 0 {
		return normUsage{}, false
	}
	return normUsage{Prompt: int64(prompt), Completion: int64(completion), CacheHit: int64(hit), CacheWrite: int64(write)}, true
}

// parseUsageRaw normalizes a bare usage JSON object (e.g. the aggregated
// usage frame of an SSE stream).
func parseUsageRaw(raw []byte) (normUsage, bool) {
	if len(raw) == 0 {
		return normUsage{}, false
	}
	var u upstreamUsage
	if err := json.Unmarshal(raw, &u); err != nil {
		return normUsage{}, false
	}
	return normalizeUpstreamUsage(&u)
}

// parseUsageBody normalizes the usage object of a complete non-streaming
// response body ({"usage": {...}}).
func parseUsageBody(body []byte) (normUsage, bool) {
	var envelope struct {
		Usage *upstreamUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Usage == nil {
		return normUsage{}, false
	}
	return normalizeUpstreamUsage(envelope.Usage)
}
