package slice

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDistillUsesTaskBodyNotRunnerInstructions(t *testing.T) {
	const task = "Why does the payment parser stall?"
	for _, input := range []string{
		"Fix the following issue in /testbed. The matching Python environment and dependencies are preinstalled at /opt/miniconda3/envs/testbed; use that Python and the existing tests.\n\n" + task,
		"<execution-policy>Fix tests first</execution-policy>\n<issue>" + task + "</issue>",
		"You are working in a git checkout of the org/repo repository.\n\nIssue:\n" + task + "\n\nRequirements:\n- Find the root cause and implement a complete fix",
		"You are solving a real GitHub issue in this repository.\n\nIssue:\n" + task,
		task + "\nExpected: returns immediately\nActual: stalls",
	} {
		t.Run(input[:min(32, len(input))], func(t *testing.T) {
			line, _ := json.Marshal(map[string]string{"role": "user", "content": input})
			transcript := append(line, []byte(`
{"role":"assistant","tool_calls":[{"id":"e","name":"edit_file","arguments":{"path":"parser.go"}},{"id":"v","name":"bash","arguments":{"command":"go test ./..."}}]}
{"role":"tool","tool_call_id":"e","content":"edited parser.go"}
{"role":"tool","tool_call_id":"v","content":"ok","verification":"passed"}
`)...)
			cards, err := Distill(transcript, SliceMeta{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, c := range cards {
				if strings.HasPrefix(string(c.Content), "Task outcome") {
					found = true
					if c.Meta.TaskType != TaskInvestigate || !strings.Contains(string(c.Content), "): "+task+"\n") {
						t.Fatalf("runner polluted card: %s", c.Content)
					}
				}
			}
			if !found {
				t.Fatal("no outcome")
			}
		})
	}
}
