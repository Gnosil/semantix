// Package lookup defines the semantix_lookup tool (U8, tool side): a
// reusable tool contract that lets any harness agent query the semantic
// slice library during a session. The kernel keeps the tool definition and
// execution here so harness forks only need a thin wrapper that forwards
// (query, limit, scope) to it.
package lookup

import (
	"fmt"

	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// ToolName is the stable tool name a harness registers.
const ToolName = "semantix_lookup"

// ToolDescription is the agent-facing description (keep concise; it is
// embedded in the model context).
const ToolDescription = "Look up previously-done work in the semantic slice library. " +
	"Use when the current task looks similar to something done before: " +
	"pass a short query describing the task; returns top slices (prompt/tool pattern/result)."

// ArgSchema mirrors the JSON schema served to tool-calling models.
type ArgSchema struct {
	Type       string            `json:"type"`
	Properties map[string]ArgProp `json:"properties"`
	Required   []string          `json:"required"`
}

// ArgProp describes one tool argument.
type ArgProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     any    `json:"default,omitempty"`
}

// Schema returns the tool argument schema.
func Schema() ArgSchema {
	return ArgSchema{
		Type: "object",
		Properties: map[string]ArgProp{
			"query": {
				Type:        "string",
				Description: "task description to match against stored slices",
			},
			"limit": {
				Type:        "integer",
				Description: "maximum number of slices to return",
				Default:     5,
			},
			"scope": {
				Type:        "string",
				Description: "retrieval scope: project (default) | user | session",
				Default:     "project",
			},
		},
		Required: []string{"query"},
	}
}

// Result is one returned slice, safe to serialize as tool output. Zone is
// the grey-zone verdict (Krites arXiv:2602.13165): hit = clearly reusable,
// grey = verify before reuse, miss = do not reuse. SourceSession records
// which session the slice was extracted from (U30; empty when unknown).
type Result struct {
	ID            string  `json:"id"`
	Type          string  `json:"type"`
	Scope         string  `json:"scope"`
	Score         float64 `json:"score"`
	Zone          string  `json:"zone"`
	Content       string  `json:"content"`
	SourceSession string  `json:"source_session"`
}

// Execute runs the lookup against idx. args uses the JSON schema names;
// unknown fields are ignored.
func Execute(idx slice.Index, args map[string]any) ([]Result, error) {
	q, _ := args["query"].(string)
	if q == "" {
		return nil, fmt.Errorf("%s: query is required", ToolName)
	}
	limit := 5
	if v, ok := args["limit"].(float64); ok && v > 0 {
		if v > 50 { // clamp before conversion: float64→int of huge values is implementation-defined
			v = 50
		}
		limit = int(v)
	}
	if limit > 50 { // hard cap: keep tool output bounded
		limit = 50
	}
	scope := slice.Project
	if v, ok := args["scope"].(string); ok {
		switch v {
		case "user":
			scope = slice.User
		case "session":
			scope = slice.Session
		case "project":
			scope = slice.Project
		}
	}
	hits, err := idx.Search(q, limit, scope)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ToolName, err)
	}
	top1 := 0.0
	if len(hits) > 0 {
		top1 = hits[0].Score
	}
	zones := zone.Default()
	out := make([]Result, 0, len(hits))
	for _, h := range hits {
		out = append(out, Result{
			ID:            h.Slice.ID,
			Type:          h.Slice.Type.String(),
			Scope:         h.Slice.Scope.String(),
			Score:         h.Score,
			Zone:          zones.ClassifyHit(h, top1).String(),
			Content:       string(h.Slice.Content),
			SourceSession: h.Slice.Meta.SourceSession,
		})
	}
	return out, nil
}
