package gateway

import (
	"encoding/json"
	"fmt"
	"log"
)

// Thinking-effort translation (GLM-P0-5, Issue #293).
//
// The two wire paths take DIFFERENT thinking parameters and do not accept
// each other's (measured on Tencent MaaS, glm-spike-week.md §2): the
// OpenAI-style path takes `reasoning_effort` ("low"|"medium"|"high"|"max"),
// while the Anthropic-style path takes `thinking:{type:"enabled",
// budget_tokens:N}` and returns HTTP 400 for `reasoning_effort`. The
// gateway's client surface is OpenAI-compatible, so requests bound for an
// anthropic-vendor upstream must translate — and after translation the
// outgoing body must be INCAPABLE of carrying reasoning_effort.
//
// effortBudgetTokens maps the effort ladder onto thinking budgets. The
// floor (1024) is the Anthropic-shape minimum budget; the ceiling mirrors
// the effort=max default of GLM-5.3 (thinking always-on, spec §3.2).
var effortBudgetTokens = map[string]int{
	"low":    1024,
	"medium": 4096,
	"high":   16384,
	"max":    32768,
}

// anthropicThinking is the thinking parameter of the Anthropic-style path.
type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// translateThinking keeps manual thinking budgets below the output limit.
// An omitted limit leaves the default output allowance after thinking.
// Explicit limits stay unchanged: generated budgets shrink to fit, while
// incompatible explicit thinking objects return an error.
func translateThinking(effort string, clientThinking json.RawMessage, maxTokens int, explicitLimit bool) (json.RawMessage, int, error) {
	if len(clientThinking) > 0 && string(clientThinking) != "null" {
		var thinking anthropicThinking
		if err := json.Unmarshal(clientThinking, &thinking); err != nil {
			return nil, maxTokens, fmt.Errorf("parse thinking: %w", err)
		}
		if thinking.Type == "enabled" {
			if thinking.BudgetTokens < 1024 {
				return nil, maxTokens, fmt.Errorf("thinking.budget_tokens must be at least 1024")
			}
			if !explicitLimit {
				maxTokens = thinking.BudgetTokens + defaultAnthropicMaxTokens
			}
			if thinking.BudgetTokens >= maxTokens {
				return nil, maxTokens, fmt.Errorf("thinking.budget_tokens must be less than the output limit (%d)", maxTokens)
			}
		}
		return clientThinking, maxTokens, nil
	}
	if effort == "" {
		return nil, maxTokens, nil
	}
	budget, ok := effortBudgetTokens[effort]
	if !ok {
		log.Printf("gateway: unknown reasoning_effort %q — forwarding without a thinking parameter (endpoint default applies)", effort)
		return nil, maxTokens, nil
	}
	if explicitLimit {
		if maxTokens <= 1024 {
			return nil, maxTokens, fmt.Errorf("output limit must exceed 1024 for enabled thinking")
		}
		budget = min(budget, maxTokens-1)
	} else {
		maxTokens = budget + defaultAnthropicMaxTokens
	}
	raw, err := json.Marshal(anthropicThinking{Type: "enabled", BudgetTokens: budget})
	return raw, maxTokens, err
}

// validateEffortForOpenAIPath warns (once per request) when the effort
// vocabulary is unknown — the OpenAI-style path forwards it verbatim, so a
// typo surfaces as an upstream 400 that is otherwise hard to attribute.
func validateEffortForOpenAIPath(effort string) {
	if effort == "" {
		return
	}
	if _, ok := effortBudgetTokens[effort]; !ok {
		log.Printf("gateway: reasoning_effort %q is outside the known ladder %v — forwarding verbatim (upstream may reject it)",
			effort, []string{"low", "medium", "high", "max"})
	}
}

// NOTE: the Anthropic path cannot leak reasoning_effort by construction —
// toAnthropicRequest re-marshals the body from anthropicRequest, which has
// no such field. TestAnthropicNeverForwardsReasoningEffort locks this in.
