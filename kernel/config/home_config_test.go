package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestHomeConfigDiscoveryAndPrecedence(t *testing.T) {
	t.Chdir(t.TempDir())
	home := t.TempDir()
	key := "HOME"
	if runtime.GOOS == "windows" {
		key = "USERPROFILE"
	}
	t.Setenv(key, home)
	t.Setenv("SEMANTIX_CONFIG", "")
	t.Setenv("SEMANTIX_DB", "")
	t.Setenv("SEMANTIX_SCOPE", "")
	homeConfig := filepath.Join(home, ".semantix", "semantix.toml")
	if err := os.MkdirAll(filepath.Dir(homeConfig), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(homeConfig, []byte("[store]\ndb = '~/shared.db'\nscope = 'user'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertField(t, r, "store.db", filepath.Join(home, "shared.db"), SourceFile)
	assertField(t, r, "store.scope", "user", SourceFile)
	if err := os.WriteFile("semantix.toml", []byte("[store]\ndb = 'local.db'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err = Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertField(t, r, "store.db", "local.db", SourceFile)
	t.Setenv("SEMANTIX_CONFIG", "~/.semantix/semantix.toml")
	r, err = Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertField(t, r, "store.db", filepath.Join(home, "shared.db"), SourceFile)
	t.Setenv("SEMANTIX_DB", "~/environment.db")
	r, err = Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertField(t, r, "store.db", filepath.Join(home, "environment.db"), SourceEnv)
	r, err = Load(Options{DB: "~/flag.db"})
	if err != nil {
		t.Fatal(err)
	}
	assertField(t, r, "store.db", filepath.Join(home, "flag.db"), SourceFlag)
	if _, err := Load(Options{ConfigPath: "missing.toml"}); err == nil {
		t.Fatal("explicit missing config fell back to another file")
	}
	t.Setenv("SEMANTIX_CONFIG", "")
	if err := os.WriteFile("semantix.toml", []byte("[broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(Options{}); err == nil {
		t.Fatal("malformed local config silently fell back to home")
	}
}
