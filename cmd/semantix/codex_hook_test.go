package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"semantix/harness/filelock"
	"semantix/kernel/slice"
)

func callCodexHook(t *testing.T, event any, dataDir string, deps dependencies) codexHookOutput {
	t.Helper()
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runCodexHook(bytes.NewReader(payload), &stdout, &stderr, dataDir, deps); code != 0 {
		t.Fatalf("hook exited %d", code)
	}
	var result codexHookOutput
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid hook output %q: %v", &stdout, err)
	}
	return result
}

func TestCodexHookCapturesAndRecallsWithinProject(t *testing.T) {
	deps := productionDependencies()
	dataDir, workspace := t.TempDir(), t.TempDir()
	nested := filepath.Join(workspace, "src")
	for _, path := range []string{filepath.Join(workspace, ".git"), nested} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	event := map[string]string{
		"hook_event_name": "UserPromptSubmit",
		"cwd":             nested,
		"session_id":      "first-session",
		"turn_id":         "first-turn",
		"prompt":          "Remember canary lighthouse zephyr protocol for integration tests.",
	}
	if got := callCodexHook(t, event, dataDir, deps); got.Context != nil {
		t.Fatal("unexpected recall on an empty store")
	}
	event["hook_event_name"] = "Stop"
	event["last_assistant_message"] = "Canary lighthouse zephyr protocol uses the integration test fixture."
	if got := callCodexHook(t, event, dataDir, deps); got.Context != nil {
		t.Fatal("Stop must not emit model context")
	}
	// Duplicate Stop must not open the store again.
	unavailable := deps
	unavailable.openStore = func(string) (slice.Store, error) {
		t.Fatal("duplicate Stop opened the store")
		return nil, nil
	}
	callCodexHook(t, event, dataDir, unavailable)

	event["hook_event_name"], event["cwd"] = "UserPromptSubmit", workspace
	event["session_id"], event["turn_id"] = "next-session", "next-turn"
	got := callCodexHook(t, event, dataDir, deps)
	if got.Context == nil || got.Context.Event != "UserPromptSubmit" ||
		!strings.Contains(got.Context.Text, "lighthouse zephyr") {
		t.Fatalf("expected recall before model request, got %+v", got.Context)
	}
	event["cwd"] = t.TempDir()
	if got := callCodexHook(t, event, dataDir, deps); got.Context != nil {
		t.Fatal("memory leaked into another project")
	}
}

func TestCodexHookCapturesLargeStopWithinInputLimit(t *testing.T) {
	dataDir, workspace := t.TempDir(), t.TempDir()
	deps := productionDependencies()
	event := codexHookEvent{
		Name: "UserPromptSubmit", CWD: workspace, Session: "session", Turn: "one",
		Prompt: "Remember the cobalt lighthouse project convention.",
	}
	callCodexHook(t, event, dataDir, deps)
	event.Name = "Stop"
	event.Response = "Synthetic fixture: " + strings.Repeat("<", 700_000)
	var input bytes.Buffer
	enc := json.NewEncoder(&input)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(event); err != nil {
		t.Fatal(err)
	}
	if input.Len() > codexHookInputLimit {
		t.Fatal("Stop fixture exceeds the hook input limit")
	}
	var stdout, stderr bytes.Buffer
	if code := runCodexHook(&input, &stdout, &stderr, dataDir, deps); code != 0 || stdout.String() != "{}\n" || stderr.Len() != 0 {
		t.Fatalf("Stop: exit=%d stdout=%q stderr=%q", code, &stdout, &stderr)
	}
	pending, err := filepath.Glob(filepath.Join(dataDir, "projects", "*", "pending", "*.txt"))
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending captures = %v, %v", pending, err)
	}
	event.Name, event.Session, event.Response = "UserPromptSubmit", "next-session", ""
	got := callCodexHook(t, event, dataDir, deps)
	if got.Context == nil || !strings.Contains(got.Context.Text, "cobalt lighthouse") {
		t.Fatalf("expected the captured prompt to be recalled, got %+v", got.Context)
	}
}

func TestCodexHookIgnoresUnavailableOrExpiredCapture(t *testing.T) {
	dataDir, workspace := t.TempDir(), t.TempDir()
	event := map[string]string{
		"hook_event_name": "UserPromptSubmit", "cwd": workspace,
		"session_id": "session", "turn_id": "one", "prompt": "Keep this project fact.",
	}
	deps := productionDependencies()
	callCodexHook(t, event, dataDir, deps)
	pending, err := filepath.Glob(filepath.Join(dataDir, "projects", "*", "pending", "*"))
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending capture = %v, %v", pending, err)
	}
	expired := time.Now().Add(-2 * codexPendingTTL)
	if err := os.Chtimes(pending[0], expired, expired); err != nil {
		t.Fatal(err)
	}
	event["hook_event_name"], event["last_assistant_message"] = "Stop", "A late answer."
	callCodexHook(t, event, dataDir, deps)
	if _, err := os.Stat(pending[0]); !os.IsNotExist(err) {
		t.Fatalf("expired capture survived: %v", err)
	}
	event["hook_event_name"], event["turn_id"] = "UserPromptSubmit", "no-answer"
	callCodexHook(t, event, dataDir, deps)
	event["hook_event_name"], event["last_assistant_message"] = "Stop", ""
	callCodexHook(t, event, dataDir, deps)
	memory, err := filepath.Glob(filepath.Join(dataDir, "projects", "*", "memory.jsonl"))
	if err != nil || len(memory) != 0 {
		t.Fatalf("incomplete turns wrote memory: %v, %v", memory, err)
	}
	var stdout bytes.Buffer
	if code := runCodexHook(strings.NewReader("{broken"), &stdout, &stdout, dataDir, deps); code != 0 || stdout.String() != "{}\n" {
		t.Fatalf("malformed input did not fail open: %d, %q", code, &stdout)
	}
}

func TestCodexHookStoreFailureDoesNotBlock(t *testing.T) {
	deps := productionDependencies()
	dataDir, workspace := t.TempDir(), t.TempDir()
	event := map[string]string{
		"hook_event_name": "UserPromptSubmit", "cwd": workspace,
		"session_id": "session", "turn_id": "one", "prompt": "Keep this project fact.",
	}
	callCodexHook(t, event, dataDir, deps)
	event["hook_event_name"], event["last_assistant_message"] = "Stop", "The project fact."
	deps.openStore = func(string) (slice.Store, error) {
		return nil, errors.New("synthetic backend failure")
	}
	if got := callCodexHook(t, event, dataDir, deps); got.Context != nil {
		t.Fatal("failed capture returned model context")
	}
	pending, err := filepath.Glob(filepath.Join(dataDir, "projects", "*", "pending", "*"))
	if err != nil || len(pending) != 1 {
		t.Fatalf("failed capture lost its retry state: %v, %v", pending, err)
	}
	locks, err := filepath.Glob(filepath.Join(dataDir, "projects", "*", "hook.lock"))
	if err != nil || len(locks) != 1 {
		t.Fatalf("project lock = %v, %v", locks, err)
	}
	release, err := filelock.TryAcquire(locks[0])
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if got := callCodexHook(t, event, dataDir, deps); got.Context != nil {
		t.Fatal("contended Stop returned model context")
	}
	if _, err := os.Stat(pending[0]); err != nil {
		t.Fatalf("contended Stop lost its retry state: %v", err)
	}
}
