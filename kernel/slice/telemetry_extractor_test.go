package slice

import (
	"strings"
	"testing"
)

func TestTelemetryDoesNotBecomeConversation(t *testing.T) {
	telemetry := []string{
		`{"kind":5,"session_id":"s","at":"2026-08-18T00:00:00Z","data":{"layer":"L3","slice_ids":["hit"]}}`,
		`{"kind":6,"session_id":"s","at":"2026-08-18T00:00:00Z","data":{"slice_ids":["inject"],"bytes":128}}`,
		`{"kind":8,"session_id":"s","at":"2026-08-18T00:00:00Z","data":{"targets":["used"]}}`,
		`{"kind":9,"session_id":"s","at":"2026-08-18T00:00:00Z","data":{"targets":["unused"]}}`,
		`{"kind":11,"session_id":"s","at":"2026-08-18T00:00:00Z","data":{"params":{"tau_l2":0.55}}}`,
	}
	genuine := []string{
		`{"role":"user","content":"Fix the slice hit counter"}`,
		`{"role":"assistant","tool_calls":[{"id":"edit","name":"edit_file","arguments":{"path":"counter.go"}}]}`,
		`{"role":"tool","tool_call_id":"edit","name":"edit_file","workspace_mutation":true,"content":"edited counter.go"}`,
		`{"role":"assistant","tool_calls":[{"id":"test","name":"exec_command","arguments":{"command":"go test ./counter"}}]}`,
		`{"type":"tool","tool_call_id":"test","name":"exec_command","verification":"passed","content":"ok"}`,
		`{"role":"assistant","content":"Fixed and tested the counter"}`,
	}
	var mixed []string
	for i, line := range genuine {
		mixed = append(mixed, line)
		if i < len(telemetry) {
			mixed = append(mixed, telemetry[i])
		}
	}
	wantTypes := []SliceType{Prompt, ToolPattern, Result}
	wantContents := []string{"Fix the slice hit counter", "edit_file exec_command", "Fixed and tested the counter"}
	for _, tc := range []struct {
		name  string
		lines []string
		count int
	}{
		{"genuine", genuine, 3},
		{"mixed", mixed, 3},
		{"telemetry_only", telemetry, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, err := NewExtractor().Extract([]byte(strings.Join(tc.lines, "\n")), SliceMeta{SourceSession: "s"})
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != tc.count {
				t.Fatalf("got %d slices, want %d", len(items), tc.count)
			}
			for i, item := range items {
				if item.Type != wantTypes[i] || string(item.Content) != wantContents[i] {
					t.Errorf("slice %d = %v %q, want %v %q", i, item.Type, item.Content, wantTypes[i], wantContents[i])
				}
				if item.Type == Result && (item.Meta.ResultStatus != ResultStatusVerified || item.Meta.ResultVerificationEvidence != "go test ./counter") {
					t.Errorf("tool verification lost: %+v", item.Meta)
				}
			}
		})
	}
}
