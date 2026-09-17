package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"semantix/harness/config"
)

func TestActivateBaseStyleClearsActivePack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SEMANTIX_HOME", home)
	app := NewApp()

	// Seed a user pack and activate it.
	m := &ThemePackManifest{
		SchemaVersion: 1, ID: "user-amber-skin", Name: "User Amber", BaseStyle: "amber",
		Recipes: defaultThemePackRecipes(),
	}
	staging, err := writeThemeStaging(m, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staging)
	if err := publishThemeDir(m.ID, staging, false); err != nil {
		t.Fatal(err)
	}
	if err := app.ActivateThemePack(m.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.ActivateBaseStyle("slate"); err != nil {
		t.Fatal(err)
	}
	exp, err := app.GetThemeExperience()
	if err != nil {
		t.Fatal(err)
	}
	if exp.ActiveThemeID != "" || exp.ActivePack != nil {
		t.Fatalf("expected no active pack after base style, got %+v", exp)
	}
	if exp.BaseStyle != "slate" || exp.EffectiveStyle != "slate" {
		t.Fatalf("base/effective = %q/%q, want slate", exp.BaseStyle, exp.EffectiveStyle)
	}
}

func TestActivateThemePackRejectsBaseStyle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SEMANTIX_HOME", home)
	app := NewApp()
	if err := app.ActivateThemePack("amber"); err == nil {
		t.Fatal("expected base style activation to fail")
	}
}

func TestMigrateV1BaseActiveIDToBaseStyle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SEMANTIX_HOME", home)
	// Write v1 state with base style as activeThemeId.
	statePath := filepath.Join(config.MemoryUserDir(), themeStateFileName)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "activeThemeId": "amber"})
	if err := os.WriteFile(statePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	exp, err := app.GetThemeExperience()
	if err != nil {
		t.Fatal(err)
	}
	if exp.ActiveThemeID != "" {
		t.Fatalf("activeThemeId should be cleared, got %q", exp.ActiveThemeID)
	}
	if exp.BaseStyle != "amber" {
		t.Fatalf("baseStyle = %q, want amber after migration", exp.BaseStyle)
	}
	// File rewritten as v2 without base id.
	st := loadThemeDesktopState()
	if st.SchemaVersion != themeStateSchemaVer || st.ActiveThemeID != "" {
		t.Fatalf("persisted state = %+v", st)
	}
}

// publishTestTheme installs a minimal user theme pack into the current
// SEMANTIX_HOME so tests have a real pack to activate.
func publishTestTheme(t *testing.T, id, baseStyle string) {
	t.Helper()
	m := &ThemePackManifest{
		SchemaVersion: 1, ID: id, Name: id, BaseStyle: baseStyle,
		Recipes: defaultThemePackRecipes(),
	}
	staging, err := writeThemeStaging(m, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staging)
	if err := publishThemeDir(m.ID, staging, false); err != nil {
		t.Fatal(err)
	}
}

func TestDisableThemePackKeepsBaseStyle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SEMANTIX_HOME", home)
	app := NewApp()
	if err := app.ActivateBaseStyle("nocturne"); err != nil {
		t.Fatal(err)
	}
	publishTestTheme(t, "keep-base", "slate")
	if err := app.ActivateThemePack("keep-base"); err != nil {
		t.Fatal(err)
	}
	if err := app.DisableThemePack(); err != nil {
		t.Fatal(err)
	}
	exp, err := app.GetThemeExperience()
	if err != nil {
		t.Fatal(err)
	}
	if exp.ActiveThemeID != "" {
		t.Fatalf("expected disabled pack, got %q", exp.ActiveThemeID)
	}
	if exp.BaseStyle != "nocturne" {
		t.Fatalf("baseStyle should stay nocturne, got %q", exp.BaseStyle)
	}
}

func TestRestoreGraphiteAppearance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SEMANTIX_HOME", home)
	app := NewApp()
	_ = app.ActivateBaseStyle("carbon")
	publishTestTheme(t, "restore-me", "carbon")
	if err := app.ActivateThemePack("restore-me"); err != nil {
		t.Fatal(err)
	}
	if err := app.RestoreGraphiteAppearance(); err != nil {
		t.Fatal(err)
	}
	exp, err := app.GetThemeExperience()
	if err != nil {
		t.Fatal(err)
	}
	if exp.BaseStyle != "graphite" || exp.ActiveThemeID != "" {
		t.Fatalf("got base=%q active=%q", exp.BaseStyle, exp.ActiveThemeID)
	}
}
