package evidence

import (
	"encoding/json"
	"testing"
)

func TestVerifierFailurePipelineRegression(t *testing.T) {
	cases := []struct {
		command string
		masked  bool
	}{
		{"cd /testbed && /opt/miniconda3/envs/testbed/bin/python -m pytest tmp_test_b.py 2>&1 | tail -10", true},
		{"pytest tests | tail -10", true}, {"pytest tests |& tail -10", true},
		{"go test ./...; echo done", true}, {"pytest || true", true}, {"pytest &", true},
		{"pytest | tail -10 && echo done", true}, {"pytest | node --check -", true},
		{"pytest && echo done", false}, {"cd /testbed && pytest", false},
		{"tail -n +1 out.json | node --check -", false},
		{"cat file.js | node --check - && echo done", false},
		{"echo done; pytest", false}, {"grep pytest file | tail -10", false},
	}
	for _, c := range cases {
		t.Run(c.command, func(t *testing.T) {
			b, _ := json.Marshal(map[string]string{"command": c.command})
			if got := BashToolCallMasksVerificationExit(b); got != c.masked {
				t.Errorf("masked=%v want=%v", got, c.masked)
			}
		})
	}
	if IsDeliveryVerificationCommand("pytest | tail -10") {
		t.Error("masked exit accepted as verification evidence")
	}

}
