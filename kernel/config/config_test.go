package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func writeTOML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "semantix.toml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func noenv(string) string { return "" }

func TestDefaultMatchesHardcoded(t *testing.T) {
	c := Default()
	if c.Store.DB != ".semantix/project.db" {
		t.Errorf("Store.DB = %q, want .semantix/project.db", c.Store.DB)
	}
	if c.Store.Scope != "project" {
		t.Errorf("Store.Scope = %q, want project", c.Store.Scope)
	}
	if c.Retrieval.Retriever != "bm25" {
		t.Errorf("Retrieval.Retriever = %q, want bm25", c.Retrieval.Retriever)
	}
	if c.Retrieval.SearchLimit != 10 {
		t.Errorf("SearchLimit = %d, want 10", c.Retrieval.SearchLimit)
	}
	if c.Retrieval.LookupLimit != 5 {
		t.Errorf("LookupLimit = %d, want 5", c.Retrieval.LookupLimit)
	}
	if c.Inject.Budget != 4096 {
		t.Errorf("Inject.Budget = %d, want 4096", c.Inject.Budget)
	}
	if c.Verify.Holdout != 0.3 {
		t.Errorf("Verify.Holdout = %v, want 0.3", c.Verify.Holdout)
	}
}

func TestLoadImplicitMissingFileUsesDefaults(t *testing.T) {
	// Point at a path that does not exist, implicitly (Explicit=false).
	cfg, src, err := Load(LoadOptions{Path: filepath.Join(t.TempDir(), "nope.toml"), Getenv: noenv})
	if err != nil {
		t.Fatalf("implicit missing file must not error: %v", err)
	}
	if cfg.Store.DB != Default().Store.DB {
		t.Errorf("db = %q, want default", cfg.Store.DB)
	}
	if len(src) != 0 {
		t.Errorf("source should be empty for implicit default, got %v", src)
	}
}

func TestLoadExplicitMissingFileErrors(t *testing.T) {
	_, _, err := Load(LoadOptions{Path: filepath.Join(t.TempDir(), "nope.toml"), Explicit: true, Getenv: noenv})
	ce, ok := IsError(err)
	if !ok {
		t.Fatalf("want *Error, got %v", err)
	}
	if !strings.Contains(ce.Msg, "not found") {
		t.Errorf("msg = %q, want file-not-found", ce.Msg)
	}
}

func TestLoadParsesAndMarksSources(t *testing.T) {
	p := writeTOML(t, "[store]\ndb = \"custom.db\"\nscope = \"user\"\n\n[retrieval]\nretriever = \"hybrid\"\nsearch_limit = 20\nlookup_limit = 7\n")
	cfg, src, err := Load(LoadOptions{Path: p, Explicit: true, Getenv: noenv})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.DB != "custom.db" || cfg.Store.Scope != "user" {
		t.Errorf("store = %+v", cfg.Store)
	}
	if cfg.Retrieval.SearchLimit != 20 || cfg.Retrieval.LookupLimit != 7 || cfg.Retrieval.Retriever != "hybrid" {
		t.Errorf("retrieval = %+v", cfg.Retrieval)
	}
	if src["store.db"] != OriginFile || src["retrieval.search_limit"] != OriginFile {
		t.Errorf("source = %v", src)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	p := writeTOML(t, "[store]\ndb = \"custom.db\"\n")
	env := func(k string) string {
		if k == "SEMANTIX_DB" {
			return "env.db"
		}
		return ""
	}
	cfg, src, err := Load(LoadOptions{Path: p, Explicit: true, Getenv: env})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.DB != "env.db" {
		t.Errorf("db = %q, want env.db", cfg.Store.DB)
	}
	if src["store.db"] != OriginEnv {
		t.Errorf("source = %v, want env for store.db", src)
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	p := writeTOML(t, "[store\nbroken")
	_, _, err := Load(LoadOptions{Path: p, Explicit: true, Getenv: noenv})
	ce, ok := IsError(err)
	if !ok {
		t.Fatalf("want *Error, got %v", err)
	}
	if ce.Line <= 0 {
		t.Errorf("want a line number on parse error, got %v", ce.Error())
	}
}

func TestLoadUnknownField(t *testing.T) {
	p := writeTOML(t, "[store]\ndb = \"x\"\nunknown_key = 1\n")
	_, _, err := Load(LoadOptions{Path: p, Explicit: true, Getenv: noenv})
	if _, ok := IsError(err); !ok {
		t.Fatalf("want *Error for unknown field, got %v", err)
	}
}

func TestLoadUnknownSection(t *testing.T) {
	p := writeTOML(t, "[nonexistent]\nfoo = \"bar\"\n")
	_, _, err := Load(LoadOptions{Path: p, Explicit: true, Getenv: noenv})
	if _, ok := IsError(err); !ok {
		t.Fatalf("want *Error for unknown section, got %v", err)
	}
}

func TestLoadTypeMismatch(t *testing.T) {
	p := writeTOML(t, "[retrieval]\nsearch_limit = \"abc\"\n")
	_, _, err := Load(LoadOptions{Path: p, Explicit: true, Getenv: noenv})
	if _, ok := IsError(err); !ok {
		t.Fatalf("want *Error for type mismatch, got %v", err)
	}
}

func TestLoadValidation(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want string // substring of the field path
	}{
		{"bad scope", "[store]\nscope = \"team\"\n", "store.scope"},
		{"bad retriever", "[retrieval]\nretriever = \"sql\"\n", "retrieval.retriever"},
		{"negative limit", "[retrieval]\nsearch_limit = -1\n", "retrieval.search_limit"},
		{"holdout out of range", "[verify]\nholdout = 1.5\n", "verify.holdout"},
		{"negative price", "[cost]\ninput_price_usd = -0.1\n", "cost.input_price_usd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeTOML(t, tc.toml)
			_, _, err := Load(LoadOptions{Path: p, Explicit: true, Getenv: noenv})
			ce, ok := IsError(err)
			if !ok {
				t.Fatalf("want *Error, got %v", err)
			}
			if !strings.Contains(ce.Field, tc.want) {
				t.Errorf("field = %q, want contains %q", ce.Field, tc.want)
			}
		})
	}
}

func TestLoadRelativePathPreserved(t *testing.T) {
	// store.db is resolved relative to the process cwd, not the config file
	// directory. Load must return the path verbatim.
	p := writeTOML(t, "[store]\ndb = \"relative/path.db\"\n")
	cfg, _, err := Load(LoadOptions{Path: p, Explicit: true, Getenv: noenv})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.DB != "relative/path.db" {
		t.Errorf("db = %q, want verbatim relative path", cfg.Store.DB)
	}
}

// TestLoadExampleTOML ensures the shipped semantix.example.toml stays parseable
// after field renames, so the "copy it to semantix.toml" workflow never breaks.
func TestLoadExampleTOML(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "semantix.example.toml"))
	if err != nil {
		t.Skipf("example toml not found from test cwd: %v", err)
	}
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		t.Fatalf("semantix.example.toml no longer parses: %v", err)
	}
	if cfg.Retrieval.SearchLimit != 10 || cfg.Retrieval.LookupLimit != 5 {
		t.Errorf("example limits = %d/%d, want 10/5", cfg.Retrieval.SearchLimit, cfg.Retrieval.LookupLimit)
	}
	if cfg.Retrieval.Retriever != "hybrid" {
		t.Errorf("example retriever = %q, want hybrid", cfg.Retrieval.Retriever)
	}
}
