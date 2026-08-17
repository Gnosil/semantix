package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// semantixLookup queries the semantix kernel's slice store via the `semantix`
// CLI subprocess. It is read-only and fails soft: when the kernel binary is
// unavailable or times out, Execute returns an empty result instead of an
// error, so a kernel outage never breaks the agent's turn.
type semantixLookup struct{}

func init() { RegisterBuiltin(semantixLookup{}) }

func (semantixLookup) Name() string { return "semantix_lookup" }

func (semantixLookup) Description() string {
	return "Search the semantix slice store — the project's past-session knowledge — for similar tasks or code snippets, and return the top matches as JSON."
}

func (semantixLookup) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "the task or question to look up"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "max hits (default 5)"}
  },
  "required": ["query"]
}`)
}

func (semantixLookup) ReadOnly() bool { return true }

// SnipHint keeps the top hits intact — the head of the JSON result carries
// the ranked answers, which is what matters for reuse.
func (semantixLookup) SnipHint() SnipHint {
	return SnipHint{Head: 20, Tail: 0}
}

func (semantixLookup) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("semantix_lookup: bad args: %w", err)
	}
	if p.Query == "" {
		return "", fmt.Errorf("semantix_lookup: query is required")
	}
	if p.Limit <= 0 || p.Limit > 50 {
		p.Limit = 5
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "semantix",
		"lookup", "--query", p.Query, "--limit", fmt.Sprint(p.Limit), "--json").Output()
	if err != nil {
		return "", nil // soft degrade: kernel unavailable or timed out
	}
	return string(out), nil
}
