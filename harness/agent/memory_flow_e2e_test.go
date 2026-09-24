package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"semantix/harness/event"
	"semantix/harness/provider"
	semantixbridge "semantix/harness/semantix"
	"semantix/harness/tool"
	"semantix/kernel/fingerprint"
	"semantix/kernel/slice"
)

// This is a transport/provenance test, not a model-benefit benchmark. Historical
// tool events include explicit host verification fixtures; the later provider
// only records the real Agent.Run request and returns a deterministic response.
func TestMemoryFlowCorroboratingResultAndOutcomeToProvider(t *testing.T) {
	repo := t.TempDir()
	paths := []string{"internal/orchard/cache.go", "internal/orchard/keys.go"}
	for _, path := range paths {
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("package orchard\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", "internal")
	git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "orchard fixture")
	revision := git("rev-parse", "HEAD")
	deps, err := fingerprint.Capture(repo, paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".semantix"), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := slice.NewFileStore(filepath.Join(repo, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	const query = "Validate orchard cache expired-entry regression"
	const verifiedCommand = "go test ./internal/orchard -run TestExpiredCacheEntry"
	wantID, resultID := "", ""
	for _, history := range []struct {
		id, task, edited, command string
	}{
		{"history-a", "Fix expired orchard cache entries", paths[0], verifiedCommand},
		{"history-b", "Fix malformed orchard cache keys", paths[1], "go test ./internal/orchard -run TestMalformedCacheKey"},
	} {
		hs, err := semantixbridge.NewHarnessSink(filepath.Join(repo, "mirrors"), history.id, "")
		if err != nil {
			t.Fatal(err)
		}
		hs.Emit(event.Event{Kind: event.TurnStarted, Text: history.task})
		hs.Emit(event.Event{Kind: event.Text, Text: "Inspect the cache implementation."})
		hs.Emit(event.Event{Kind: event.Message, Text: "Inspect the cache implementation."})
		emitTool := func(id, name string, args map[string]string, output string, mutation, verified bool) {
			t.Helper()
			encoded, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			hs.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: id, Name: name, Args: string(encoded)}})
			result := event.Tool{ID: id, Name: name, Output: output, WorkspaceMutation: mutation}
			if verified {
				result.Execution = &event.ShellExecution{State: "exited", Verification: "passed"}
			}
			hs.Emit(event.Event{Kind: event.ToolResult, Tool: result})
		}
		// Both sessions inspect the same subsystem independently. Different edit
		// counts produce distinct extracted Context cards that can really merge.
		for i, path := range paths {
			for read := 0; read < 2; read++ {
				emitTool(fmt.Sprintf("read-%d-%d", i, read), "read_file", map[string]string{"path": path}, "package orchard", false, false)
			}
		}
		emitTool("edit", "edit_file", map[string]string{"path": history.edited}, "edited "+history.edited, true, false)
		emitTool("verify", "bash", map[string]string{"command": history.command}, "ok orchard tests", false, true)
		final := "Completed: " + history.task + ". Verified with " + history.command + "."
		hs.Emit(event.Event{Kind: event.Text, Text: final})
		hs.Emit(event.Event{Kind: event.Message, Text: final})
		hs.EndTurn()
		if err := hs.Close(); err != nil {
			t.Fatal(err)
		}
		transcript, err := os.ReadFile(filepath.Join(repo, "mirrors", history.id+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		meta := slice.SliceMeta{SourceSession: history.id, ProjectSlug: "orchard", BaseCommit: revision,
			Origin: slice.OriginSessionAuto, TaskType: slice.ClassifyTask(history.task), Deps: deps}
		items, err := slice.NewExtractor().Extract(transcript, meta)
		if err != nil {
			t.Fatal(err)
		}
		foundResult := false
		for _, item := range items {
			if item.Type == slice.Result {
				foundResult = true
				if history.id == "history-a" {
					resultID = item.ID
					t.Logf("corroborating Result %s metadata=%+v content:\n%s", item.ID, item.Meta, item.Content)
				}
				if string(item.Content) != final || item.Meta.EffectiveResultStatus() != slice.ResultStatusVerified {
					t.Fatalf("%s lost its separate verified final answer: %+v", history.id, item)
				}
			}
		}
		if !foundResult {
			t.Fatalf("%s mirror produced no Result after tool use", history.id)
		}
		cards, err := slice.Distill(transcript, meta)
		if err != nil {
			t.Fatal(err)
		}
		for _, prefix := range []string{"Repo operations", "Plan skeleton", "Task outcome"} {
			found := false
			for _, card := range cards {
				found = found || strings.HasPrefix(string(card.Content), prefix)
			}
			if !found {
				t.Fatalf("%s did not distill its %s layer", history.id, prefix)
			}
		}
		for _, card := range cards {
			if history.id == "history-a" && strings.HasPrefix(string(card.Content), "Task outcome") {
				if !strings.Contains(string(card.Content), "Verified-by: "+verifiedCommand) {
					t.Fatalf("outcome lost host-verified command: %s", card.Content)
				}
				wantID = card.ID
				t.Logf("corroborating outcome %s metadata=%+v content:\n%s", card.ID, card.Meta, card.Content)
			}
		}
		for _, item := range append(items, cards...) {
			existing, err := store.Get(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			slice.RetainExtractionHistory(item, existing)
			if err := store.Put(item); err != nil {
				t.Fatal(err)
			}
		}
	}
	if wantID == "" {
		t.Fatal("historical event pipeline produced no outcome card")
	}
	merged, err := slice.ConsolidateContext(store, slice.ConsolidateOptions{})
	if err != nil || merged.Merged == 0 {
		t.Fatalf("real Context consolidation = %+v, %v", merged, err)
	}
	mergedSources := false
	for _, id := range merged.Created {
		card, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(card.Meta)
		if err != nil {
			t.Fatal(err)
		}
		var provenance struct {
			Sources []string `json:"source_sessions"`
		}
		if err := json.Unmarshal(encoded, &provenance); err != nil {
			t.Fatal(err)
		}
		if strings.Join(provenance.Sources, ",") == "history-a,history-b" {
			mergedSources = true
		}
	}
	if !mergedSources {
		t.Fatal("Context consolidation lost the two independent source sessions")
	}
	all, err := store.List(slice.Project)
	if err != nil || len(all) < 5 {
		t.Fatalf("extracted library has %d slices: %v", len(all), err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	var offMessages []byte
	for _, mode := range []string{"off", "shadow", "strict"} {
		t.Run(mode, func(t *testing.T) {
			request, block, diagnostics := memoryFlowRun(t, repo, mode, query)
			encoded, err := json.Marshal(request.Messages)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "strict" {
				if block != "" || strings.Contains(string(encoded), "[semantix-reuse]") || strings.Contains(string(encoded), verifiedCommand) {
					t.Fatalf("%s leaked historical knowledge: %s", mode, encoded)
				}
				if mode == "off" {
					offMessages = encoded
					return
				}
				if string(encoded) != string(offMessages) {
					t.Fatalf("shadow changed provider input:\noff=%s\nshadow=%s", offMessages, encoded)
				}
				if diagnostics == nil || diagnostics.Injected || diagnostics.Bytes != 0 || diagnostics.MessageRole != "" || diagnostics.DecisionReason != "shadow_mode" {
					t.Fatalf("shadow did not withhold historical knowledge: %+v", diagnostics)
				}
			} else if diagnostics == nil || diagnostics.Decision != "assembled" || diagnostics.Bytes != len(block) || diagnostics.MessageRole != "user" || diagnostics.DecisionReason != "admitted" || block == "" {
				t.Fatalf("strict injection absent or wrong bytes/role: %+v", diagnostics)
			}
			var result, outcome *event.RetrievalCandidate
			for i := range diagnostics.Candidates {
				candidate := &diagnostics.Candidates[i]
				switch candidate.ID {
				case resultID:
					result = candidate
				case wantID:
					outcome = candidate
				}
			}
			// These are two representations of the same verified history, not
			// competing answers. Their near tie must not veto the whole block.
			if result == nil || outcome == nil || !result.Admitted || !outcome.Admitted || result.SourceSession != "history-a" || outcome.SourceSession != "history-a" || result.Verified != "verified" {
				t.Fatalf("corroborating Result/outcome were not admitted: %+v", diagnostics)
			}
			margin := result.Score - outcome.Score
			if margin <= 0 || margin >= 0.15 || diagnostics.TopMargin != margin {
				t.Fatalf("actual score gap = %g, diagnostic gap = %g; want the near-tied fixture", margin, diagnostics.TopMargin)
			}
			for _, id := range []string{resultID, wantID} {
				if !slices.Contains(diagnostics.FinalOrder, id) {
					t.Fatalf("final order lost corroborating slice %s: %+v", id, diagnostics)
				}
			}
			if mode == "shadow" {
				return
			}
			for _, want := range []string{"--- slice " + resultID + " ---", "--- slice " + wantID + " ---", `source="history-a"`, "Verified-by: " + verifiedCommand, `commit="` + revision + `"`, `origin=session-auto`} {
				if !strings.Contains(block, want) {
					t.Fatalf("history lacks %q:\n%s", want, block)
				}
			}
			foundHistory, foundPolicy := false, false
			for _, message := range request.Messages {
				if strings.Contains(message.Content, verifiedCommand) && message.Role != provider.RoleUser {
					t.Fatalf("knowledge elevated to %s", message.Role)
				}
				foundHistory = foundHistory || strings.Contains(message.Content, block)
				foundPolicy = foundPolicy || (message.Role == provider.RoleSystem && strings.Contains(message.Content, semantixHistoryPolicy))
			}
			if !foundHistory || !foundPolicy {
				t.Fatalf("provider lacks untrusted-reference placement: %s", encoded)
			}
			t.Logf("query=%q library=%d merged=%d actual margin=%g bytes=%d role=user\nprovider history:\n%s\ndiagnostics=%+v", query, len(all), merged.Merged, margin, len(block), block, diagnostics)
		})
	}
}

// Runs the real agent and records exactly the request sent to its provider.
// No synthetic injection wrapper or retrieval implementation participates.
func memoryFlowRun(t *testing.T, repo, mode, query string) (provider.Request, string, *event.RetrievalDiagnostics) {
	t.Helper()
	bridge := semantixbridge.NewBridge(semantixbridge.Config{Enabled: true, Inject: true, Mode: mode,
		ProjectDir: repo, WorkspaceDir: repo, SessionsDir: filepath.Join(repo, "later-mirrors")})
	defer bridge.Close()
	observed := make(chan *event.RetrievalDiagnostics, 8)
	sink := bridge.Sink(event.FuncSink(func(e event.Event) {
		if e.KernelCache != nil && e.KernelCache.Retrieval != nil {
			select {
			case observed <- e.KernelCache.Retrieval:
			default:
			}
		}
	}))
	recorder := &recordingProvider{reply: "done"}
	a := New(recorder, tool.NewRegistry(), NewSession("You are a coding assistant."), Options{Semantix: bridge}, sink)
	composed := "<capability-route>Use the available workspace tools.</capability-route>\n\n" + query
	if err := a.Run(WithRawUserInput(context.Background(), query), composed); err != nil {
		t.Fatal(err)
	}
	if len(recorder.got) != 1 {
		t.Fatalf("recorded %d provider requests, want one", len(recorder.got))
	}
	var diagnostics *event.RetrievalDiagnostics
	select {
	case diagnostics = <-observed:
	default:
		if mode != "off" {
			t.Fatal("real Agent.Run emitted no retrieval diagnostics")
		}
	}
	return recorder.got[0], a.turn.injectBlock, diagnostics
}

// Replays the same two tool histories and unchanged later task used by the
// bounded invoice live smoke. The historical commands really run here, too;
// only the later provider is a recorder. No claim of model benefit is made.
func TestMemoryFlowInvoiceHistoryToProvider(t *testing.T) {
	const query = "Fix invoice total calculation for fractional quantities. total_lines currently truncates quantity to int; " +
		"it must accept fractional quantities while preserving Decimal arithmetic and rounding the final invoice total " +
		"to two decimal places. Add regression tests and run the existing invoice test suite. " +
		"Work only in invoice.py and tests/."
	const command = "python3 -m unittest discover -s tests -p 'test_invoice*.py' -v"
	repo := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("invoice.py", `from decimal import Decimal, ROUND_HALF_UP


def total_lines(lines):
    total = sum((Decimal(str(price)) * int(quantity) for price, quantity in lines), Decimal("0"))
    return total.quantize(Decimal("0.01"), rounding=ROUND_HALF_UP)
`)
	write("README.md", `# Invoice totals

This small invoice module computes Decimal line totals. Run the standard-library
invoice suite from the repository root:

    python3 -m unittest discover -s tests -p 'test_invoice*.py' -v

No package installation is needed. Source lives in invoice.py; tests live in tests/.
`)
	write("tests/test_invoice_basic.py", `import unittest
from decimal import Decimal
from invoice import total_lines

class InvoiceBasicTests(unittest.TestCase):
    def test_whole_quantity(self):
        self.assertEqual(total_lines([("2.25", 3)]), Decimal("6.75"))
`)
	write(".gitignore", "__pycache__/\n")
	run := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %s: %v", name, args, out, err)
		}
		return string(out)
	}
	run("git", "init", "-q")
	run("git", "add", ".")
	run("git", "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Invoice subsystem fixture")
	revision := strings.TrimSpace(run("git", "rev-parse", "HEAD"))
	deps, err := fingerprint.Capture(repo, []string{"invoice.py"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".semantix"), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := slice.NewFileStore(filepath.Join(repo, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	wantID := ""
	for _, history := range []struct{ id, task, name, class, input string }{
		{"prior-zero", "Add invoice total regression tests for zero quantity; locate the invoice suite and verify it.", "zero", "Zero", `[("12.50", 0)]`},
		{"prior-empty", "Add invoice regression tests for empty invoices; locate the invoice suite and verify it.", "empty", "Empty", "[]"},
	} {
		hs, err := semantixbridge.NewHarnessSink(filepath.Join(repo, "mirrors"), history.id, "")
		if err != nil {
			t.Fatal(err)
		}
		hs.Emit(event.Event{Kind: event.TurnStarted, Text: history.task})
		emit := func(id, name string, args map[string]string, execute func() string, mutation, verify bool) {
			t.Helper()
			encoded, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			hs.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: id, Name: name, Args: string(encoded)}})
			result := event.Tool{ID: id, Name: name, Output: execute(), WorkspaceMutation: mutation}
			if verify {
				zero := 0
				result.Execution = &event.ShellExecution{State: "exited", ExitCode: &zero, Verification: "passed"}
			}
			hs.Emit(event.Event{Kind: event.ToolResult, Tool: result})
		}
		emit(history.id+"-1", "bash", map[string]string{"command": "cat README.md invoice.py"},
			func() string { return run("bash", "-lc", "cat README.md invoice.py") }, false, false)
		path := "tests/test_invoice_" + history.name + ".py"
		content := fmt.Sprintf("import unittest\nfrom decimal import Decimal\nfrom invoice import total_lines\n\nclass Invoice%sTests(unittest.TestCase):\n    def test_%s(self):\n        self.assertEqual(total_lines(%s), Decimal(\"0.00\"))\n", history.class, history.name, history.input)
		emit(history.id+"-2", "write_file", map[string]string{"path": path, "content": content}, func() string {
			write(path, content)
			return fmt.Sprintf("wrote %d bytes to %s\n", len(content), path)
		}, true, false)
		emit(history.id+"-3", "bash", map[string]string{"command": command},
			func() string { return run("bash", "-lc", command) }, false, true)
		final := "Added the " + history.name + " invoice regression test. The invoice suite passed."
		hs.Emit(event.Event{Kind: event.Text, Text: final})
		hs.Emit(event.Event{Kind: event.Message, Text: final})
		hs.EndTurn()
		if err := hs.Close(); err != nil {
			t.Fatal(err)
		}
		transcript, err := os.ReadFile(filepath.Join(repo, "mirrors", history.id+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		meta := slice.SliceMeta{SourceSession: history.id, ProjectSlug: "repo", BaseCommit: revision,
			Origin: slice.OriginSessionAuto, TaskType: slice.ClassifyTask(history.task), Deps: deps}
		items, err := slice.NewExtractor().Extract(transcript, meta)
		if err != nil {
			t.Fatal(err)
		}
		foundResult := false
		for _, item := range items {
			if item.Type == slice.Result && string(item.Content) == final && item.Meta.EffectiveResultStatus() == slice.ResultStatusVerified {
				foundResult = true
			}
		}
		if !foundResult {
			t.Fatalf("%s lost the separate host-verified Result", history.id)
		}
		cards, err := slice.Distill(transcript, meta)
		if err != nil {
			t.Fatal(err)
		}
		for _, prefix := range []string{"Repo operations", "Plan skeleton", "Task outcome"} {
			found := false
			for _, card := range cards {
				if strings.HasPrefix(string(card.Content), prefix) {
					found = true
					if prefix == "Task outcome" && history.id == "prior-zero" {
						wantID = card.ID
					}
				}
			}
			if !found {
				t.Fatalf("%s lost %s layer", history.id, prefix)
			}
		}
		for _, item := range append(items, cards...) {
			existing, err := store.Get(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			// Match the CLI's extraction upsert, including multi-source evidence.
			slice.RetainExtractionHistory(item, existing)
			if err := store.Put(item); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := slice.ConsolidateContext(store, slice.ConsolidateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := store.List(slice.Project)
	if err != nil || len(all) < 5 || wantID == "" {
		t.Fatalf("incomplete historical library: count=%d outcome=%s err=%v", len(all), wantID, err)
	}
	foundShared := false
	for _, card := range all {
		if card.Type == slice.Context && strings.Join(card.Meta.SourceSessions, ",") == "prior-empty,prior-zero" {
			foundShared = true
		}
	}
	if !foundShared {
		t.Fatal("production extraction upsert lost the repeated Context's two real sources")
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	var offMessages []byte
	for _, mode := range []string{"off", "shadow", "strict"} {
		t.Run(mode, func(t *testing.T) {
			request, block, diagnostics := memoryFlowRun(t, repo, mode, query)
			encoded, err := json.Marshal(request.Messages)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "strict" {
				if block != "" || strings.Contains(string(encoded), "[semantix-reuse]") || strings.Contains(string(encoded), command) {
					t.Fatalf("%s leaked historical knowledge: %s", mode, encoded)
				}
				if mode == "off" {
					offMessages = encoded
				} else {
					if string(encoded) != string(offMessages) {
						t.Fatalf("shadow changed provider messages:\noff=%s\nshadow=%s", offMessages, encoded)
					}
					if diagnostics == nil || diagnostics.Injected || diagnostics.Bytes != 0 || diagnostics.DecisionReason != "shadow_mode" || len(diagnostics.FinalOrder) == 0 {
						t.Fatalf("shadow did not record the withheld candidate: %+v", diagnostics)
					}
				}
				return
			}
			if diagnostics == nil || diagnostics.Decision != "assembled" || diagnostics.Bytes != len(block) || diagnostics.MessageRole != "user" || len(block) == 0 {
				t.Fatalf("strict injection absent or wrong bytes/role: %+v", diagnostics)
			}
			for _, want := range []string{"--- slice " + wantID + " ---", `source="prior-zero"`, "Verified-by: " + command, "tests/test_invoice_zero.py", `commit="` + revision + `"`, `origin=session-auto`} {
				if !strings.Contains(block, want) {
					t.Fatalf("history lacks %q:\n%s\ndiagnostics=%+v", want, block, diagnostics)
				}
			}
			foundHistory, foundPolicy := false, false
			for _, message := range request.Messages {
				if strings.Contains(message.Content, block) {
					if message.Role != provider.RoleUser {
						t.Fatalf("knowledge elevated to %s", message.Role)
					}
					foundHistory = true
				}
				if message.Role == provider.RoleSystem && strings.Contains(message.Content, semantixHistoryPolicy) {
					foundPolicy = true
				}
			}
			if !foundHistory || !foundPolicy {
				t.Fatalf("provider lacks untrusted-reference placement: %s", encoded)
			}
			t.Logf("query=%q library=%d source=prior-zero slice=%s bytes=%d role=user\nprovider history:\n%s\ndiagnostics=%+v", query, len(all), wantID, len(block), block, diagnostics)
		})
	}

	// This is a freshness-mechanism control, not a new SWE task or a claim
	// of model benefit. Only an unrelated file changes between real commits;
	// the same historical invoice dependency and original query stay intact.
	t.Run("cross_commit_unchanged_dependencies", func(t *testing.T) {
		write("unrelated-audit-note.txt", "Unrelated documentation for the freshness control.\n")
		run("git", "add", "unrelated-audit-note.txt")
		run("git", "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Unrelated freshness control")
		current := strings.TrimSpace(run("git", "rev-parse", "HEAD"))
		if current == revision {
			t.Fatal("freshness control did not change the real Git commit")
		}
		changed := strings.TrimSpace(run("git", "diff", "--name-only", revision, current))
		if changed != "unrelated-audit-note.txt" {
			t.Fatalf("control changed relevant files: %s", changed)
		}
		if changed, err := fingerprint.Verify(repo, deps); err != nil || len(changed) != 0 {
			t.Fatalf("historical invoice dependencies changed: %v, %v", changed, err)
		}
		request, block, diagnostics := memoryFlowRun(t, repo, "strict", query)
		if diagnostics == nil || diagnostics.Decision != "assembled" || diagnostics.BaseCommit != current || diagnostics.Bytes != len(block) {
			t.Fatalf("cross-commit matching dependencies did not pass production Bridge: %+v", diagnostics)
		}
		for _, want := range []string{"--- slice " + wantID + " ---", `commit="` + revision + `"`, `origin=session-auto`, "Verified-by: " + command} {
			if !strings.Contains(block, want) {
				t.Fatalf("cross-commit history lost %q:\n%s", want, block)
			}
		}
		found := false
		for _, message := range request.Messages {
			found = found || (message.Role == provider.RoleUser && strings.Contains(message.Content, block))
		}
		if !found {
			t.Fatal("cross-commit history did not reach provider as user-role reference")
		}
		t.Logf("freshness-only control: source_commit=%s current_commit=%s changed=%s unchanged_deps=%v slice=%s bytes=%d diagnostics=%+v", revision, current, changed, deps, wantID, len(block), diagnostics)
	})
}
