package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadForRootSemantixPlaybook(t *testing.T) {
	dir := t.TempDir()
	tomlText := "[semantix]\nenabled = true\ninject = true\nplaybook = true\nrepo_short = \"django\"\n"
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(tomlText), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Semantix.Playbook {
		t.Errorf("Semantix.Playbook = false, want true (loaded from project reasonix.toml)")
	}
	if cfg.Semantix.RepoShort != "django" {
		t.Errorf("Semantix.RepoShort = %q, want django", cfg.Semantix.RepoShort)
	}
	if !cfg.Semantix.Inject {
		t.Errorf("Semantix.Inject = false, want true")
	}
}
