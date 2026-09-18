package slice

import (
	"strings"
	"testing"
)

func TestExtractionVerificationParity(t *testing.T) {
	const start = `{"role":"user","content":"Fix parser regression"}
{"role":"assistant","tool_calls":[{"id":"edit","name":"edit_file","arguments":{"path":"parser.go"}}]}
{"type":"tool","tool_call_id":"edit","name":"edit_file","content":"edited parser.go","workspace_mutation":true}
{"role":"assistant","tool_calls":[{"id":"pass","name":"bash","arguments":{"command":"go test ./..."}}]}
{"type":"tool","tool_call_id":"pass","name":"bash","content":"ok","verification":"passed"}
`
	const next = `{"role":"assistant","tool_calls":[{"id":"next","name":"bash","arguments":{"command":"go test ./..."}}]}` + "\n"
	const final = `{"role":"assistant","content":"The parser change is complete."}` + "\n"
	for _, tc := range []struct {
		name, suffix string
		verified     bool
	}{
		{"host_pass", "", true},
		{"pass_then_timeout", next + `{"type":"tool","tool_call_id":"next","name":"bash","content":"command timed out"}` + "\n", false},
		{"pass_then_unknown", next + `{"type":"tool","tool_call_id":"next","name":"bash","content":"all tests passed","verification":"unknown"}` + "\n", false},
		{"pass_then_cancel", `{"role":"assistant","tool_calls":[{"id":"next","name":"run_tests"}]}
{"type":"tool","tool_call_id":"next","name":"run_tests","content":"context canceled"}
`, false},
		{"pass_then_unittest_timeout", `{"role":"assistant","tool_calls":[{"id":"next","name":"bash","arguments":{"command":"python3 -m unittest discover -s tests"}}]}
{"type":"tool","tool_call_id":"next","name":"bash","content":"command timed out"}
`, false},
		{"pass_then_missing_result", next, false},
		{"pass_then_failed", next + `{"type":"tool","tool_call_id":"next","name":"bash","verification":"failed"}` + "\n", false},
		{"pass_then_read", `{"role":"assistant","tool_calls":[{"id":"read","name":"read_file","arguments":{"path":"parser.go"}}]}
{"type":"tool","tool_call_id":"read","name":"read_file","content":"package parser"}
`, true},
		{"unknown_then_new_host_pass", next + `{"type":"tool","tool_call_id":"next","name":"bash","content":"command timed out"}
{"role":"assistant","tool_calls":[{"id":"last","name":"run_tests"}]}
{"type":"tool","tool_call_id":"last","name":"run_tests","content":"ok","verification":"passed"}
`, true},
		{"pass_duplicate_after_mutation", `{"role":"assistant","tool_calls":[{"id":"edit2","name":"edit_file","arguments":{"path":"parser.go"}}]}
{"type":"tool","tool_call_id":"edit2","name":"edit_file","content":"edited parser.go","workspace_mutation":true}
{"type":"tool","tool_call_id":"pass","name":"bash","content":"ok","verification":"passed"}
`, false},
		{"duplicate_dispatch_after_pass", `{"role":"assistant","tool_calls":[{"id":"pass","name":"bash","arguments":{"command":"go test ./..."}}]}` + "\n", true},
		{"pass_then_nonverification_shell", `{"role":"assistant","tool_calls":[{"id":"read","name":"bash","arguments":{"command":"git diff --stat"}}]}
{"type":"tool","tool_call_id":"read","name":"bash","content":"parser.go | 1 +","verification":"not_verification"}
`, true},
		{"pass_then_read_pytest_config", `{"role":"assistant","tool_calls":[{"id":"read","name":"bash","arguments":{"command":"cat pytest.ini"}}]}
{"type":"tool","tool_call_id":"read","name":"bash","content":"[pytest]","verification":"not_verification"}
`, true},
		{"pass_then_diff_test_path", `{"role":"assistant","tool_calls":[{"id":"read","name":"bash","arguments":{"command":"git diff --stat tests/test_pytest.py"}}]}
{"type":"tool","tool_call_id":"read","name":"bash","content":"tests/test_pytest.py | 1 +","verification":"not_verification"}
`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(start + tc.suffix + final)
			extracted, err := NewExtractor().Extract(input, SliceMeta{})
			if err != nil {
				t.Fatal(err)
			}
			result := sliceOfType(extracted, Result)
			if result == nil {
				t.Fatal("missing Result")
			}
			got := result.Meta.EffectiveResultStatus() == ResultStatusVerified
			if got != tc.verified {
				t.Errorf("Result verified=%v want %v; evidence=%q", got, tc.verified, result.Meta.ResultVerificationEvidence)
			}
			if !tc.verified && (result.Meta.ResultVerifiedBy != "" || result.Meta.ResultVerificationEvidence != "") {
				t.Error("probation Result retained verification evidence")
			}
			cards, err := Distill(input, SliceMeta{})
			if err != nil {
				t.Fatal(err)
			}
			var outcome string
			for _, card := range cards {
				if strings.HasPrefix(string(card.Content), "Task outcome") {
					outcome = string(card.Content)
				}
			}
			if outcome == "" {
				t.Fatal("missing outcome Memory")
			}
			if got := strings.Contains(outcome, "Verified-by:"); got != tc.verified {
				t.Errorf("Memory verified=%v want %v: %s", got, tc.verified, outcome)
			}
		})
	}
}
