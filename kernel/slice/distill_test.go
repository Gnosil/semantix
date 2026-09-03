package slice

import (
	"strings"
	"testing"
)

func TestClassifyTask(t *testing.T) {
	cases := []struct {
		text, want string
	}{
		{"Fix the crash when parsing empty input", TaskBugfix},
		{"Rational calc error on string args", TaskBugfix},
		{"修复登录页面的报错", TaskBugfix},
		{"The failing test in CI needs an update", TaskTestUpdate},
		{"Add support for HTTP/3", TaskFeature},
		{"Add prefix support", TaskFeature},
		{"Implement suffix handling", TaskFeature},
		{"新增导出功能", TaskFeature},
		{"Refactor the retry loop into a helper", TaskRefactor},
		{"Update the README and docstrings", TaskDocs},
		{"Why does the scheduler stall under load?", TaskInvestigate},
		{"", TaskGeneral},
		{"hello world", TaskGeneral},
	}
	for _, c := range cases {
		if got := ClassifyTask(c.text); got != c.want {
			t.Errorf("ClassifyTask(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestDistillAcceptsStandardRoleToolLines(t *testing.T) {
	transcript := []byte(`{"role":"user","content":"Fix the parser crash"}
{"role":"assistant","tool_calls":[{"id":"e1","name":"edit_file","arguments":{"path":"parser.go"}},{"id":"t1","name":"bash","arguments":{"command":"go test ./..."}}]}
{"role":"tool","tool_call_id":"e1","name":"edit_file","content":"edited parser.go"}
{"role":"tool","tool_call_id":"t1","name":"bash","content":"ok"}
`)
	cards, err := Distill(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	var sawOps, sawOutcome bool
	for _, card := range cards {
		sawOps = sawOps || strings.HasPrefix(string(card.Content), "Repo operations")
		sawOutcome = sawOutcome || strings.HasPrefix(string(card.Content), "Task outcome")
	}
	if !sawOps || !sawOutcome {
		t.Fatalf("standard role=tool results were ignored: repo-ops=%v outcome=%v", sawOps, sawOutcome)
	}
}

func TestDistillNormalizesWindowsEditedPaths(t *testing.T) {
	transcript := []byte("" +
		`{"role":"user","content":"Fix the parser crash"}` + "\n" +
		`{"role":"assistant","tool_calls":[{"id":"e1","name":"edit_file","arguments":{"path":"D:\\repo\\parser.go"}},{"id":"t1","name":"bash","arguments":{"command":"cd D:\\repo && go test ./..."}}]}` + "\n" +
		`{"type":"tool","tool_call_id":"e1","name":"edit_file","content":"edited D:\\repo\\parser.go"}` + "\n" +
		`{"type":"tool","tool_call_id":"t1","name":"bash","content":"ok"}` + "\n")
	cards, err := Distill(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range cards {
		if strings.HasPrefix(string(card.Content), "Task outcome") {
			if strings.Contains(string(card.Content), `D:\repo`) || !strings.Contains(string(card.Content), "- parser.go") {
				t.Fatalf("outcome kept absolute Windows path:\n%s", card.Content)
			}
			return
		}
	}
	t.Fatal("missing outcome card")
}

func TestClassifyTaskPrecedence(t *testing.T) {
	// A text mentioning both a failing test and a fix is a test task: the
	// more specific intent wins over the broader bugfix keyword set.
	if got := ClassifyTask("fix the failing test in test_numbers"); got != TaskTestUpdate {
		t.Fatalf("precedence: got %q, want %q", got, TaskTestUpdate)
	}
}

// distillFixture mirrors the real harness mirror shape: kernel event lines,
// role lines with streamed (double-written) tool calls, and duplicated tool
// result lines.
const distillFixture = `{"kind":5,"session_id":"m","at":"2026-08-30T21:32:00Z","data":{"layer":"L2","slice_ids":["aa"]}}
{"role":"user","content":"You are solving a real GitHub issue in this repository.\n\nIssue:\nRational('0.5', '100') returns the wrong value — fix the bug.\n"}
{"role":"assistant","content":"working","tool_calls":[{"id":"c1","name":"bash","arguments":{}},{"id":"c1","name":"bash","arguments":{"command":"cd /tmp/run/repos/sympy && rg -n \"class Rational\" sympy/core/numbers.py"}},{"id":"c2","name":"write_file","arguments":{"path":"repro_issue.py"}},{"id":"c3","name":"bash","arguments":{"command":"cd /tmp/run/repos/sympy && python repro_issue.py"}},{"id":"c4","name":"edit_file","arguments":{"path":"sympy/core/numbers.py"}},{"id":"c5","name":"bash","arguments":{"command":"cd /tmp/run/repos/sympy && python -m pytest sympy/core/tests/test_numbers.py -x"}},{"id":"c6","name":"bash","arguments":{"command":"cd /tmp/run/repos/sympy && python -m pytest sympy/core/tests/test_numbers.py -x"}},{"id":"c7","name":"bash","arguments":{"command":"cd /tmp/run/repos/sympy && pip install -e ."}}]}
{"type":"tool","tool_call_id":"c1","name":"bash","content":"1483:class Rational(Number):"}
{"type":"tool","tool_call_id":"c1","name":"bash","content":"1483:class Rational(Number):"}
{"type":"tool","tool_call_id":"c2","name":"write_file","content":"wrote 322 bytes to /tmp/run/repos/sympy/repro_issue.py"}
{"type":"tool","tool_call_id":"c3","name":"bash","content":"blocked: non-terminal inline interpreter command"}
{"type":"tool","tool_call_id":"c4","name":"edit_file","content":"edited /tmp/run/repos/sympy/sympy/core/numbers.py\nActual replacement receipt after write:"}
{"type":"tool","tool_call_id":"c4","name":"edit_file","content":"edited /tmp/run/repos/sympy/sympy/core/numbers.py\nActual replacement receipt after write:"}
{"type":"tool","tool_call_id":"c5","name":"bash","content":"FAILED test_numbers.py::test_x\ncommand exited: exit status 1"}
{"type":"tool","tool_call_id":"c6","name":"bash","content":"3 passed in 1.2s"}
{"type":"tool","tool_call_id":"c7","name":"bash","content":"Successfully installed sympy"}
{"role":"assistant","content":"Done — fixed the Rational constructor."}
`

func TestDistillCards(t *testing.T) {
	slices, err := Distill([]byte(distillFixture), SliceMeta{SourceSession: "s1"})
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	var repoOps, skeleton, outcome *Slice
	for _, s := range slices {
		content := string(s.Content)
		switch {
		case strings.HasPrefix(content, "Repo operations"):
			repoOps = s
		case strings.HasPrefix(content, "Plan skeleton"):
			skeleton = s
		case strings.HasPrefix(content, "Task outcome"):
			outcome = s
		}
	}

	if repoOps == nil {
		t.Fatal("missing repo-ops card")
	}
	if repoOps.Type != Context {
		t.Errorf("repo-ops type = %v, want Context", repoOps.Type)
	}
	ops := string(repoOps.Content)
	// The failing run is superseded by the later passing run of the same
	// normalized command (freshest signal wins), cd prefix stripped.
	if !strings.Contains(ops, "python -m pytest sympy/core/tests/test_numbers.py -x (ok)") {
		t.Errorf("repo-ops missing verified test command:\n%s", ops)
	}
	if strings.Contains(ops, "exit 1") {
		t.Errorf("repo-ops kept the stale failing outcome:\n%s", ops)
	}
	if !strings.Contains(ops, "pip install -e . (ok)") {
		t.Errorf("repo-ops missing build command:\n%s", ops)
	}
	if !strings.Contains(ops, "blocked: non-terminal inline interpreter command (seen 1)") {
		t.Errorf("repo-ops missing pitfall:\n%s", ops)
	}
	if strings.Contains(ops, "cd /tmp/run") {
		t.Errorf("repo-ops kept cd noise:\n%s", ops)
	}

	if skeleton == nil {
		t.Fatal("missing plan-skeleton card")
	}
	if skeleton.Type != Memory {
		t.Errorf("skeleton type = %v, want Memory", skeleton.Type)
	}
	if skeleton.Meta.TaskType != TaskBugfix {
		t.Errorf("skeleton TaskType = %q, want bugfix", skeleton.Meta.TaskType)
	}
	sk := string(skeleton.Content)
	if !strings.Contains(sk, "task=bugfix") {
		t.Errorf("skeleton missing task tag:\n%s", sk)
	}
	// c1 locate → c2 reproduce (repro path) → c3 reproduce (folded ×2) →
	// c4 edit → c5/c6 verify (folded ×2); c7 build carries no stage.
	if !strings.Contains(sk, "locate → reproduce(×2) → edit → verify(×2)") {
		t.Errorf("unexpected skeleton trajectory:\n%s", sk)
	}

	if outcome == nil {
		t.Fatal("missing outcome card")
	}
	if outcome.Type != Memory {
		t.Errorf("outcome type = %v, want Memory", outcome.Type)
	}
	oc := string(outcome.Content)
	if !strings.Contains(oc, "task=bugfix") {
		t.Errorf("outcome missing task tag:\n%s", oc)
	}
	if !strings.Contains(oc, "Rational('0.5', '100') returns the wrong value") {
		t.Errorf("outcome summary should quote the Issue section:\n%s", oc)
	}
	if !strings.Contains(oc, "- sympy/core/numbers.py") {
		t.Errorf("outcome missing root-stripped edited path:\n%s", oc)
	}
	if !strings.Contains(oc, "- repro_issue.py") {
		t.Errorf("outcome missing wrote-receipt path:\n%s", oc)
	}
	if strings.Count(oc, "sympy/core/numbers.py") != 1 {
		t.Errorf("outcome should dedup double-written edit receipts:\n%s", oc)
	}
	if !strings.Contains(oc, "Verified-by: python -m pytest sympy/core/tests/test_numbers.py -x") {
		t.Errorf("outcome missing verifying command:\n%s", oc)
	}
}

func TestDistillEmptyAndNoSignal(t *testing.T) {
	slices, err := Distill(nil, SliceMeta{})
	if err != nil || len(slices) != 0 {
		t.Fatalf("empty input: got %d slices, err %v", len(slices), err)
	}
	// A chat-only session (no tool calls, no edits) yields no cards: there
	// is nothing operational to remember.
	chat := `{"role":"user","content":"hello"}` + "\n" + `{"role":"assistant","content":"hi"}` + "\n"
	slices, err = Distill([]byte(chat), SliceMeta{})
	if err != nil {
		t.Fatalf("chat-only: %v", err)
	}
	for _, s := range slices {
		t.Errorf("chat-only session produced unexpected card: %s", s.Content)
	}
}

func TestDistillDeterminism(t *testing.T) {
	a, err := Distill([]byte(distillFixture), SliceMeta{SourceSession: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Distill([]byte(distillFixture), SliceMeta{SourceSession: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("nondeterministic card count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID || string(a[i].Content) != string(b[i].Content) {
			t.Errorf("card %d differs across runs", i)
		}
	}
}

func TestDistillRespectsPresetTaskType(t *testing.T) {
	slices, err := Distill([]byte(distillFixture), SliceMeta{TaskType: "custom"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range slices {
		if s.Meta.TaskType != "custom" {
			t.Errorf("preset TaskType overwritten: got %q", s.Meta.TaskType)
		}
	}
}
