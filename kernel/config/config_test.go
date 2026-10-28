package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/zone"
)

func assertField(t *testing.T, r *Resolved, key string, want any, wantSrc Source) {
	t.Helper()
	v, src, ok := r.Get(key)
	if !ok {
		t.Fatalf("field %q missing", key)
	}
	if v != want {
		t.Errorf("field %q = %v (%T), want %v (%T)", key, v, v, want, want)
	}
	if src != wantSrc {
		t.Errorf("field %q source = %q, want %q", key, src, wantSrc)
	}
}

func TestLoadDefaults(t *testing.T) {
	r, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "project.name", "my-project", SourceDefault)
	assertField(t, r, "store.db", "", SourceDefault)
	assertField(t, r, "store.scope", "project", SourceDefault)
	assertField(t, r, "retrieval.retriever", "hybrid", SourceDefault)
	assertField(t, r, "retrieval.limit", 5, SourceDefault)
	assertField(t, r, "inject.budget", 4096, SourceDefault)
	assertField(t, r, "verify.holdout", 0.3, SourceDefault)
	assertField(t, r, "cost.output_price_usd", 1.10, SourceDefault)
}

func TestLoadFileOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	content := "[store]\ndb = \"/tmp/custom.db\"\nscope = \"user\"\n\n[retrieval]\nlimit = 20\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", "/tmp/custom.db", SourceFile)
	assertField(t, r, "store.scope", "user", SourceFile)
	assertField(t, r, "retrieval.limit", 20, SourceFile)
	// keys the file did not define fall back to default
	assertField(t, r, "retrieval.retriever", "hybrid", SourceDefault)
	assertField(t, r, "inject.budget", 4096, SourceDefault)
}

func TestLoadPartialSectionFallsBackToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte("[store]\ndb = \"/tmp/only-db.db\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", "/tmp/only-db.db", SourceFile)
	assertField(t, r, "store.scope", "project", SourceDefault)
	assertField(t, r, "retrieval.limit", 5, SourceDefault)
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("SEMANTIX_DB", "/env/user.db")
	t.Setenv("SEMANTIX_LIMIT", "42")
	r, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", "/env/user.db", SourceEnv)
	assertField(t, r, "retrieval.limit", 42, SourceEnv)
}

func TestLoadEnvEmptyStringFallsThrough(t *testing.T) {
	t.Setenv("SEMANTIX_DB", "")
	r, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", "", SourceDefault)
}

func TestLoadFlagOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte("[store]\ndb = \"/file.db\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEMANTIX_DB", "/env.db")
	r, err := Load(Options{ConfigPath: path, DB: "/flag.db"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// flag beats env beats file
	assertField(t, r, "store.db", "/flag.db", SourceFlag)
}

func TestLoadEnvBeatsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte("[store]\ndb = \"/file.db\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEMANTIX_DB", "/env.db")
	r, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", "/env.db", SourceEnv)
}

func TestLoadInvalidScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte("[store]\nscope = \"team\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(Options{ConfigPath: path})
	if err == nil {
		t.Fatal("Load: want error for invalid scope")
	}
	if !strings.Contains(err.Error(), "store.scope") {
		t.Fatalf("error %q should locate store.scope", err)
	}
}

func TestLoadInvalidEnvInteger(t *testing.T) {
	t.Setenv("SEMANTIX_LIMIT", "abc")
	_, err := Load(Options{})
	if err == nil {
		t.Fatal("Load: want error for invalid env integer")
	}
	if !strings.Contains(err.Error(), "retrieval.limit") {
		t.Fatalf("error %q should locate retrieval.limit", err)
	}
}

func TestLoadTypeErrorReportsLocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	if err := os.WriteFile(path, []byte("[retrieval]\nlimit = \"not-a-number\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(Options{ConfigPath: path})
	if err == nil {
		t.Fatal("Load: want error for string into int field")
	}
	// BurntSushi/toml reports line/column in its error
	if !strings.Contains(err.Error(), "toml") && !strings.Contains(err.Error(), "line") {
		t.Fatalf("error %q should carry location info", err)
	}
}

func TestLoadExplicitMissingFile(t *testing.T) {
	_, err := Load(Options{ConfigPath: filepath.Join(t.TempDir(), "nope.toml")})
	if err == nil {
		t.Fatal("Load: want error for explicit missing file")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error %q should report missing file", err)
	}
}

func TestLoadDefaultMissingFileSkipped(t *testing.T) {
	// default ./semantix.toml does not exist in the test cwd → skip, not error
	dir := t.TempDir()
	t.Chdir(dir)
	r, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "store.db", "", SourceDefault)
}

func TestLoadZoneThresholdsFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	content := "[retrieval]\ntau_high = 0.9\ntau_low = 0.6\nabs_high = 0.8\nabs_low = 0.5\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "retrieval.tau_high", 0.9, SourceFile)
	assertField(t, r, "retrieval.tau_low", 0.6, SourceFile)
	assertField(t, r, "retrieval.abs_high", 0.8, SourceFile)
	assertField(t, r, "retrieval.abs_low", 0.5, SourceFile)
}

func TestLoadZoneThresholdsDefaultToZoneDefaults(t *testing.T) {
	r, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	def := zone.Default()
	assertField(t, r, "retrieval.tau_high", def.TauHigh, SourceDefault)
	assertField(t, r, "retrieval.tau_low", def.TauLow, SourceDefault)
	assertField(t, r, "retrieval.abs_high", def.AbsHigh, SourceDefault)
	assertField(t, r, "retrieval.abs_low", def.AbsLow, SourceDefault)
}

func TestLoadRejectsInvalidZoneThresholds(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"tau_high zero", "[retrieval]\ntau_high = 0\n", "retrieval.tau_high: must be in (0,1]"},
		{"tau_low over one", "[retrieval]\ntau_low = 1.5\n", "retrieval.tau_low: must be in (0,1]"},
		{"abs_high negative", "[retrieval]\nabs_high = -1\n", "retrieval.abs_high: must be >= 0"},
		{"tau_high not above tau_low", "[retrieval]\ntau_high = 0.6\ntau_low = 0.6\n", "retrieval.tau_high (0.6) must be > retrieval.tau_low (0.6)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "semantix.toml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(Options{ConfigPath: path})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLoadByTypeOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantix.toml")
	content := "[retrieval]\ntau_high = 0.8\n\n[retrieval.by_type.result]\ntau_high = 0.85\ntau_low = 0.60\n\n[retrieval.by_type.context]\ntau_low = 0.50\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertField(t, r, "retrieval.by_type.result.tau_high", 0.85, SourceFile)
	assertField(t, r, "retrieval.by_type.result.tau_low", 0.60, SourceFile)
	assertField(t, r, "retrieval.by_type.context.tau_low", 0.50, SourceFile)
	// Global key untouched by the per-type block.
	assertField(t, r, "retrieval.tau_high", 0.8, SourceFile)
	// Deterministic field order: context sorts before result.
	keys := []string{}
	for _, f := range r.Fields {
		if f.Key == "retrieval.by_type.context.tau_low" || f.Key == "retrieval.by_type.result.tau_high" {
			keys = append(keys, f.Key)
		}
	}
	if len(keys) != 2 || keys[0] != "retrieval.by_type.context.tau_low" || keys[1] != "retrieval.by_type.result.tau_high" {
		t.Fatalf("by_type field order = %v, want sorted by type name", keys)
	}
}

func TestLoadRejectsInvalidByType(t *testing.T) {
	cases := []struct{ name, content, want string }{
		{"unknown type", "[retrieval.by_type.reslut]\ntau_low = 0.5\n", `"reslut" is not a slice type`},
		{"bad tau", "[retrieval.by_type.result]\ntau_high = 2\n", "retrieval.by_type.result.tau_high: must be in (0,1]"},
		{"bad abs", "[retrieval.by_type.result]\nabs_low = -0.5\n", "retrieval.by_type.result.abs_low: must be >= 0"},
		{"tau cross", "[retrieval.by_type.result]\ntau_high = 0.5\ntau_low = 0.6\n", "retrieval.by_type.result.tau_high (0.5) must be > tau_low (0.6)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "semantix.toml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(Options{ConfigPath: path})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsNonFiniteThresholds(t *testing.T) {
	cases := []struct{ name, content string }{
		{"global tau nan", "[retrieval]\ntau_high = nan\n"},
		{"global abs inf", "[retrieval]\nabs_low = inf\n"},
		{"by_type tau nan", "[retrieval.by_type.result]\ntau_low = nan\n"},
		{"by_type abs inf", "[retrieval.by_type.context]\nabs_high = -inf\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "semantix.toml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(Options{ConfigPath: path}); err == nil {
				t.Fatalf("Load must reject non-finite %s", tc.name)
			}
		})
	}
}
