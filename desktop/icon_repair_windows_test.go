//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepairExistingWindowsShortcutsRepairsOnlyExistingFiles(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "Semantix.lnk")
	custom := filepath.Join(root, "Custom.lnk")
	missing := filepath.Join(root, "Missing.lnk")
	if err := os.WriteFile(existing, []byte("old shortcut"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(custom, []byte("custom shortcut"), 0o600); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "semantix-launcher.exe")
	var wrote, notified []string
	originalNotify := windowsNotifyShortcutChange
	windowsNotifyShortcutChange = func(path string) { notified = append(notified, path) }
	t.Cleanup(func() { windowsNotifyShortcutChange = originalNotify })

	err := repairExistingWindowsShortcuts(
		[]string{existing, custom, missing},
		launcher,
		func(path, target string) (bool, error) {
			if target != launcher {
				t.Fatalf("shortcut target = %q, want %q", target, launcher)
			}
			wrote = append(wrote, path)
			return path == existing, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrote) != 2 || wrote[0] != existing || wrote[1] != custom {
		t.Fatalf("repair attempts = %v, want %q and %q", wrote, existing, custom)
	}
	if len(notified) != 1 || notified[0] != existing {
		t.Fatalf("shell notifications = %v, want only %q", notified, existing)
	}
}

func TestSemantixWindowsShortcutTargetRequiresCurrentInstall(t *testing.T) {
	root := filepath.Join(`C:\Program Files`, "Semantix")
	launcher := filepath.Join(root, "semantix-launcher.exe")
	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "launcher", target: launcher, want: true},
		{name: "flat desktop", target: filepath.Join(root, "semantix-desktop.exe"), want: true},
		{name: "launcher alias", target: filepath.Join(root, "Semantix.exe"), want: true},
		{name: "versioned desktop", target: filepath.Join(root, "versions", "v1.19.3", "semantix-desktop.exe"), want: true},
		{name: "other install", target: filepath.Join(`D:\Apps`, "Semantix", "semantix-launcher.exe"), want: false},
		{name: "separate 0.53 install", target: filepath.Join(`D:\Legacy`, "Semantix", "semantix-desktop.exe"), want: false},
		{name: "unrelated app", target: filepath.Join(root, "other.exe"), want: false},
		{name: "empty", target: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := semantixWindowsShortcutTarget(tt.target, launcher); got != tt.want {
				t.Fatalf("semantixWindowsShortcutTarget(%q, %q) = %v, want %v", tt.target, launcher, got, tt.want)
			}
		})
	}
}

func TestSemantixWindowsStaleIcon(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Program Files, Inc", "Semantix")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "semantix-launcher.exe")
	flat := filepath.Join(root, "semantix-desktop.exe")
	tests := []struct {
		name string
		icon string
		want bool
	}{
		{name: "versioned", icon: filepath.Join(root, "versions", "v1.19.3", "semantix-desktop.exe") + ",0", want: true},
		{name: "quoted versioned", icon: `"` + filepath.Join(root, "versions", "v1.19.3", "semantix-desktop.exe") + `", 0`, want: true},
		{name: "legacy root-level (file gone)", icon: flat + ",0", want: true},
		{name: "stable launcher", icon: launcher + ",0", want: false},
		{name: "custom icon", icon: filepath.Join(root, "custom.ico") + ",0", want: false},
		{name: "other install", icon: filepath.Join(`D:\Apps`, "Semantix", "versions", "v1.19.3", "semantix-desktop.exe") + ",0", want: false},
		{name: "empty", icon: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := semantixWindowsStaleIcon(tt.icon, launcher, false); got != tt.want {
				t.Fatalf("semantixWindowsStaleIcon(%q, %q) = %v, want %v", tt.icon, launcher, got, tt.want)
			}
		})
	}

	if err := os.WriteFile(flat, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if semantixWindowsStaleIcon(flat+",0", launcher, false) {
		t.Fatal("live flat icon = stale, want healthy")
	}
	if !semantixWindowsStaleIcon(flat+",0", launcher, true) {
		t.Fatal("versioned layout with leftover flat icon = healthy, want stale")
	}
}

func TestSemantixWindowsFlatDesktopTargetNeedsMissingFile(t *testing.T) {
	root := t.TempDir()
	launcher := filepath.Join(root, "semantix-launcher.exe")
	flat := filepath.Join(root, "semantix-desktop.exe")

	// No file on disk: the legacy root-level target dangles after the
	// versioned-layout migration and must be repointed.
	if !semantixWindowsFlatDesktopTarget(flat, launcher, false) {
		t.Fatalf("missing flat desktop target = false, want true")
	}

	// A live flat install still has the binary; its shortcut must be left alone.
	if err := os.WriteFile(flat, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if semantixWindowsFlatDesktopTarget(flat, launcher, false) {
		t.Fatalf("existing flat desktop target = true, want false")
	}
	if !semantixWindowsFlatDesktopTarget(flat, launcher, true) {
		t.Fatalf("versioned layout with leftover flat desktop target = false, want true")
	}

	// Non-flat targets never match.
	for _, target := range []string{
		launcher,
		filepath.Join(root, "Semantix.exe"),
		filepath.Join(root, "versions", "v1.19.3", "semantix-desktop.exe"),
	} {
		if semantixWindowsFlatDesktopTarget(target, launcher, true) {
			t.Fatalf("semantixWindowsFlatDesktopTarget(%q) = true, want false", target)
		}
	}
}

func TestSemantixWindowsVersionedTarget(t *testing.T) {
	root := filepath.Join(`C:\Program Files`, "Semantix")
	launcher := filepath.Join(root, "semantix-launcher.exe")
	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "versioned desktop", target: filepath.Join(root, "versions", "v1.19.3", "semantix-desktop.exe"), want: true},
		{name: "case-insensitive", target: filepath.Join(root, "Versions", "V1.19.3", "Semantix-Desktop.exe"), want: true},
		{name: "launcher", target: launcher, want: false},
		{name: "launcher alias", target: filepath.Join(root, "Semantix.exe"), want: false},
		{name: "flat desktop", target: filepath.Join(root, "semantix-desktop.exe"), want: false},
		{name: "deeper versioned path", target: filepath.Join(root, "versions", "v1.19.3", "sub", "semantix-desktop.exe"), want: false},
		{name: "wrong binary in versions", target: filepath.Join(root, "versions", "v1.19.3", "semantix-cli.exe"), want: false},
		{name: "other install", target: filepath.Join(`D:\Apps`, "Semantix", "versions", "v1.19.3", "semantix-desktop.exe"), want: false},
		{name: "empty", target: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := semantixWindowsVersionedTarget(tt.target, launcher); got != tt.want {
				t.Fatalf("semantixWindowsVersionedTarget(%q, %q) = %v, want %v", tt.target, launcher, got, tt.want)
			}
		})
	}
}

func TestRepairWindowsShortcutPlan(t *testing.T) {
	root := t.TempDir()
	launcher := filepath.Join(root, "semantix-launcher.exe")
	versioned := filepath.Join(root, "versions", "v1.19.3", "semantix-desktop.exe")
	tests := []struct {
		name        string
		target      string
		icon        string
		wantRepoint bool
		wantFixIcon bool
	}{
		{name: "versioned target + versioned icon", target: versioned, icon: versioned + ",0", wantRepoint: true, wantFixIcon: true},
		{name: "versioned target + clean icon", target: versioned, icon: launcher + ",0", wantRepoint: true, wantFixIcon: false},
		{name: "stable target + versioned icon", target: launcher, icon: versioned + ",0", wantRepoint: false, wantFixIcon: true},
		{name: "stable target + clean icon", target: launcher, icon: launcher + ",0", wantRepoint: false, wantFixIcon: false},
		{name: "flat target + flat icon (both gone)", target: filepath.Join(root, "semantix-desktop.exe"), icon: filepath.Join(root, "semantix-desktop.exe") + ",0", wantRepoint: true, wantFixIcon: true},
		{name: "flat target + clean icon", target: filepath.Join(root, "semantix-desktop.exe"), icon: launcher + ",0", wantRepoint: true, wantFixIcon: false},
		{name: "custom target + custom icon", target: filepath.Join(root, "custom.ico"), icon: filepath.Join(root, "custom.ico") + ",0", wantRepoint: false, wantFixIcon: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoint, fixIcon := repairWindowsShortcutPlan(tt.target, tt.icon, launcher, false)
			if repoint != tt.wantRepoint || fixIcon != tt.wantFixIcon {
				t.Fatalf("repairWindowsShortcutPlan(%q, %q) = (%v, %v), want (%v, %v)", tt.target, tt.icon, repoint, fixIcon, tt.wantRepoint, tt.wantFixIcon)
			}
		})
	}

	flat := filepath.Join(root, "semantix-desktop.exe")
	if err := os.WriteFile(flat, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if repoint, fixIcon := repairWindowsShortcutPlan(flat, flat+",0", launcher, false); repoint || fixIcon {
		t.Fatalf("live flat plan = (%v, %v), want (false, false)", repoint, fixIcon)
	}
	if repoint, fixIcon := repairWindowsShortcutPlan(flat, flat+",0", launcher, true); !repoint || !fixIcon {
		t.Fatalf("versioned layout leftover plan = (%v, %v), want (true, true)", repoint, fixIcon)
	}
}
