package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExplicitConfigPathForms covers the four ways a config path can be
// selected, including both --config spellings required by the CLI contract.
func TestExplicitConfigPathForms(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		env      map[string]string
		wantPath string
		wantExp  bool
	}{
		{"flag space", []string{"--config", "a.toml"}, nil, "a.toml", true},
		{"flag equals", []string{"--config=b.toml"}, nil, "b.toml", true},
		{"env wins when no flag", []string{"--query", "x"}, map[string]string{"SEMANTIX_CONFIG": "c.toml"}, "c.toml", true},
		{"flag beats env", []string{"--config", "d.toml"}, map[string]string{"SEMANTIX_CONFIG": "c.toml"}, "d.toml", true},
		{"none", []string{"--query", "x"}, nil, "", false},
		{"bare --config is explicit empty", []string{"--config"}, nil, "", true},
		{"positional stops scan", []string{"query", "--config", "e.toml"}, nil, "", false},
		{"flag value before config", []string{"--input", "session.jsonl", "--config", "f.toml"}, nil, "f.toml", true},
		{"boolean flag then config", []string{"--json", "--config", "g.toml"}, nil, "g.toml", true},
		{"inline value then config", []string{"--query=hello", "--config", "h.toml"}, nil, "h.toml", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(k string) string {
				if tc.env == nil {
					return ""
				}
				return tc.env[k]
			}
			got, exp := explicitConfigPath(tc.args, getenv)
			if got != tc.wantPath || exp != tc.wantExp {
				t.Errorf("explicitConfigPath = (%q, %v), want (%q, %v)", got, exp, tc.wantPath, tc.wantExp)
			}
		})
	}
}

// TestConfigInjectionViaQueryBlocked: a query text that contains a
// --config=... token must NOT redirect the config file (flag parse treats
// everything after the first positional as positional).
func TestConfigInjectionViaQueryBlocked(t *testing.T) {
	dir := t.TempDir()
	decoy := filepath.Join(dir, "decoy.toml")
	if err := os.WriteFile(decoy, []byte("[store]\ndb = \""+filepath.ToSlash(filepath.Join(dir, "decoy.db"))+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The query is positional; --config=... is part of the query text and
	// must be ignored for config selection. No real semantix.toml exists, so
	// the implicit default must apply (search runs against the default store).
	var out, errOut bytes.Buffer
	code := run([]string{"search", "query text --config=" + decoy}, &out, &errOut, productionDependencies())
	if code == 2 && strings.Contains(errOut.String(), "config") {
		t.Fatalf("query text redirected config selection: code=%d stderr=%s", code, errOut.String())
	}
	// The search itself may fail (no store) with exit 1, but never a config
	// exit-2, proving the decoy file was not consulted.
	if code == 2 {
		t.Fatalf("unexpected config error: %s", errOut.String())
	}
}

// TestConfigMissingExplicitReturns2: a user-specified config path that does
// not exist is a config error (exit 2), never a silent fallback to defaults.
func TestConfigMissingExplicitReturns2(t *testing.T) {
	var out, errOut bytes.Buffer
	missing := filepath.Join(t.TempDir(), "nope.toml")
	code := run([]string{"search", "--config", missing, "query"}, &out, &errOut, productionDependencies())
	if code != 2 {
		t.Fatalf("code = %d, want 2 (stderr: %s)", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "not found") {
		t.Errorf("stderr lacks file-not-found: %s", errOut.String())
	}
}

// TestLookupConfigErrorNoPartialOutput: config errors must not emit a partial
// JSON body (H1 harness contract: non-zero exit + no half-structured output).
func TestLookupConfigErrorNoPartialOutput(t *testing.T) {
	var out, errOut bytes.Buffer
	missing := filepath.Join(t.TempDir(), "nope.toml")
	code := run([]string{"lookup", "--config", missing, "--query", "q"}, &out, &errOut, productionDependencies())
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must be empty on config error, got: %s", out.String())
	}
}

// TestConfigStoreDBWired verifies store.db drives the project database end to
// end: extract writes to the configured path, search reads from it, with no
// --db/--project-db flags.
func TestConfigStoreDBWired(t *testing.T) {
	dir := t.TempDir()
	db := filepath.ToSlash(filepath.Join(dir, "wired.db"))
	cfgFile := filepath.Join(dir, "semantix.toml")
	if err := os.WriteFile(cfgFile, []byte("[store]\ndb = \""+db+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte(`{"role":"user","content":"修复 go 测试失败"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"extract", "--input", session, "--config", cfgFile}, &out, &errOut, productionDependencies()); code != 0 {
		t.Fatalf("extract code = %d, stderr: %s", code, errOut.String())
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("db not created at configured path %s: %v", db, err)
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"search", "--config", cfgFile, "修复"}, &out, &errOut, productionDependencies()); code != 0 {
		t.Fatalf("search code = %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "修复") {
		t.Fatalf("search did not read configured db:\n%s", out.String())
	}
}

// TestUsageDBUnaffectedByStoreAndEnv: usage --db is the usage JSONL log, not
// the slice store; it must ignore [store].db and SEMANTIX_DB.
func TestUsageDBUnaffectedByStoreAndEnv(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "usage.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(dir, "semantix.toml")
	if err := os.WriteFile(cfgFile, []byte("[store]\ndb = \"decoy.db\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEMANTIX_DB", "env-decoy.db")

	var out bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--config", cfgFile}, &out); code != 0 {
		t.Fatalf("usage code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "events\t0") {
		t.Fatalf("usage output unexpected:\n%s", out.String())
	}
}
