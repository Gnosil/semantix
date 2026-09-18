package slice

import (
	"strings"
	"testing"
)

func TestDistillRequiresHostVerification(t *testing.T) {
	const start = `{"role":"user","content":"Fix the parser crash"}
{"role":"assistant","tool_calls":[{"id":"e1","name":"edit_file","arguments":{"path":"parser.go"}},{"id":"t1","name":"bash","arguments":{"command":"go test ./..."}}]}
{"type":"tool","tool_call_id":"e1","name":"edit_file","content":"edited parser.go","workspace_mutation":true}
`
	for _, tc := range []struct {
		name, result, suffix, wantObservation string
		wantVerified                          bool
	}{
		{"text_only_pass", `"content":"all tests passed"`, "", "(unknown)", false},
		{"empty_output", `"content":""`, "", "(unknown)", false},
		{"timeout", `"content":"command timed out"`, "", "(unknown)", false},
		{"cancelled", `"content":"context canceled"`, "", "(unknown)", false},
		{"text_exit_zero", `"content":"exit status 0"`, "", "(exit 0)", false},
		{"text_exit_failure", `"content":"exit status 1"`, "", "(exit 1)", false},
		{"host_pass", `"content":"all tests passed","verification":"passed"`, "", "(ok)", true},
		{"host_fail", `"content":"all tests passed","verification":"failed"`, "", "(failed)", false},
		{"pass_then_mutation", `"content":"ok","verification":"passed"`, `
{"role":"assistant","tool_calls":[{"id":"e2","name":"edit_file","arguments":{"path":"parser.go"}}]}
{"role":"tool","tool_call_id":"e2","name":"edit_file","content":"edited parser.go","workspace_mutation":true}
`, "(ok)", false},
		{"pass_then_failure", `"content":"ok","verification":"passed"`, `
{"role":"assistant","tool_calls":[{"id":"t2","name":"bash","arguments":{"command":"go test ./..."}}]}
{"role":"tool","tool_call_id":"t2","name":"bash","content":"test failed","verification":"failed"}
`, "(failed)", false},
		{"pass_then_unknown", `"content":"ok","verification":"passed"`, `
{"role":"assistant","tool_calls":[{"id":"t2","name":"bash","arguments":{"command":"go test ./..."}}]}
{"role":"tool","tool_call_id":"t2","name":"bash","content":"command timed out"}
`, "(unknown)", false},
		{"pass_then_dedicated_test_cancelled", `"content":"ok","verification":"passed"`, `
{"role":"assistant","tool_calls":[{"id":"t2","name":"run_tests","arguments":{}}]}
{"type":"tool","tool_call_id":"t2","name":"run_tests","content":"context canceled"}
`, "(ok)", false},
		{"pass_then_test_without_result", `"content":"ok","verification":"passed"`, `
{"role":"assistant","tool_calls":[{"id":"t2","name":"bash","arguments":{"command":"go test ./..."}}]}
`, "(ok)", false},
		{"duplicate_pass_after_mutation", `"content":"ok","verification":"passed"`, `
{"role":"assistant","tool_calls":[{"id":"e2","name":"edit_file","arguments":{"path":"parser.go"}}]}
{"type":"tool","tool_call_id":"e2","name":"edit_file","content":"edited parser.go","workspace_mutation":true}
{"type":"tool","tool_call_id":"t1","name":"bash","content":"ok","verification":"passed"}
`, "(ok)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := start + `{"type":"tool","tool_call_id":"t1","name":"bash",` + tc.result + "}\n" + tc.suffix
			cards, err := Distill([]byte(input), SliceMeta{})
			if err != nil {
				t.Fatal(err)
			}
			var ops, outcome string
			for _, card := range cards {
				content := string(card.Content)
				if strings.HasPrefix(content, "Repo operations") {
					ops = content
				}
				if strings.HasPrefix(content, "Task outcome") {
					outcome = content
				}
			}
			if !strings.Contains(ops, "Observed commands:") || !strings.Contains(ops, "go test ./... "+tc.wantObservation) {
				t.Errorf("wrong command observation:\n%s", ops)
			}
			if !strings.Contains(outcome, "- parser.go") {
				t.Fatalf("lost the actual edit observation:\n%s", outcome)
			}
			if got := strings.Contains(outcome, "Verified-by: go test ./..."); got != tc.wantVerified {
				t.Errorf("Verified-by = %v, want %v:\n%s", got, tc.wantVerified, outcome)
			}
		})
	}
}
