package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writePortableFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyWindowsPortableVersionedLayout(t *testing.T) {
	verify := filepath.Join("..", "scripts", "verify-windows-portable.sh")
	good := t.TempDir()
	// versioned-v1 root entries
	writePortableFixture(t, good, "semantix-launcher.exe", "launcher")
	writePortableFixture(t, good, "Semantix.exe", "launcher")
	writePortableFixture(t, good, "semantix-cli.exe", "cli")
	ver := filepath.Join(good, "versions", "v1.20.0")
	if err := os.MkdirAll(ver, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"semantix-desktop.exe", "semantix-cli.exe", "semantix-update-helper.exe"} {
		writePortableFixture(t, ver, name, name)
	}
	if err := os.WriteFile(filepath.Join(good, "current.json"), []byte(`{
  "schemaVersion": 1,
  "activeVersion": "v1.20.0",
  "activeDir": "versions/v1.20.0"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", verify, good).CombinedOutput(); err != nil {
		t.Fatalf("valid versioned portable failed: %v\n%s", err, out)
	}

	// Flat Guard layout must be rejected.
	flat := t.TempDir()
	for _, name := range []string{
		"semantix-desktop.exe",
		"semantix-guard.exe",
		"semantix-update-helper.exe",
		"semantix-launcher.exe",
		"Semantix.exe",
		"semantix-cli.exe",
	} {
		writePortableFixture(t, flat, name, name)
	}
	if out, err := exec.Command("bash", verify, flat).CombinedOutput(); err == nil {
		t.Fatalf("flat portable with guard should fail, output=%s", out)
	}
}

func TestDesktopPackagesPreserveNativePlatformLaunchers(t *testing.T) {
	buildData, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	build := string(buildData)
	for _, want := range []string{
		`CLINAME="semantix"`,
		`WINDOWS_CLINAME="semantix-cli"`,
		`./cmd/semantix`,
		`./cmd/semantix-legacy-migrator`,
		`./cmd/semantix-launcher`,
		`cp "$cli_out" "$app/Contents/MacOS/$CLINAME"`,
		`macOS bundle must not include $GUARDNAME`,
		`[ "$bundle_executable" = "$BINNAME" ]`,
		`Print :CFBundleIconFile`,
		`darwin_icon="$ROOT/desktop/build/darwin/icon.icns"`,
		`cp "$darwin_icon" "$app/Contents/Resources/$bundle_icon"`,
		`cmp -s "$darwin_icon" "$app/Contents/Resources/$bundle_icon"`,
		`Contents/Resources/$bundle_icon`,
		`-H windowsgui`,
		`stamp_windows_executable "$guard_out" "Semantix Legacy Migrator"`,
		`stamp_windows_executable "$launcher_out" "Semantix Launcher"`,
		`stamp_windows_executable "build/windows/installer/$UPDATE_HELPER" "Semantix Update Helper"`,
		`payload_dir="$ROOT/desktop/build/windows/signing-payload"`,
		`cp "build/bin/$BINNAME.exe" "$payload_dir/$BINNAME.exe"`,
		`cp "$launcher_out" "$payload_dir/$LAUNCHERNAME.exe"`,
		`cp "$guard_out" "$payload_dir/$GUARDNAME.exe"`,
		`cp "build/windows/installer/$WINDOWS_CLINAME.exe" "$payload_dir/$WINDOWS_CLINAME.exe"`,
		`cp "build/windows/installer/semantix-uninstall.exe" "$payload_dir/semantix-uninstall.exe"`,
		`"$ROOT/scripts/package-windows-desktop.sh" "$arch" "$payload_dir"`,
		`"$BINNAME" "$LAUNCHERNAME" "$GUARDNAME" "$CLINAME"`,
		`Exec=semantix-launcher`,
	} {
		if !strings.Contains(build, want) {
			t.Errorf("desktop-build.sh missing packaging contract %q", want)
		}
	}
	if strings.Contains(build, `Set :CFBundleExecutable $GUARDNAME`) {
		t.Fatal("macOS package must not replace the native Wails bundle executable with Guard")
	}
	launcherStamp := strings.Index(build, `stamp_windows_executable "$launcher_out" "Semantix Launcher"`)
	payloadCopy := strings.Index(build, `cp "$launcher_out" "$payload_dir/$LAUNCHERNAME.exe"`)
	if launcherStamp < 0 || payloadCopy < 0 || launcherStamp > payloadCopy {
		t.Fatalf("Windows payload must copy the already-stamped launcher (stamp=%d copy=%d)", launcherStamp, payloadCopy)
	}
	if strings.Contains(build, `"$staging/$CLINAME.exe"`) {
		t.Fatal("Windows package must not collide semantix.exe with the Semantix.exe launcher")
	}
	darwinIconCopy := strings.Index(build, `cp "$darwin_icon" "$app/Contents/Resources/$bundle_icon"`)
	developerIDSign := strings.Index(build, `codesign --force --deep --timestamp --options runtime`)
	if darwinIconCopy < 0 || developerIDSign < 0 || darwinIconCopy > developerIDSign {
		t.Fatalf("macOS icon must be replaced before signing (icon=%d sign=%d)", darwinIconCopy, developerIDSign)
	}

	for _, want := range []string{
		`dpkg-deb --field "$deb_path" Package | grep -x 'semantix-desktop'`,
		`usr/lib/semantix/semantix-update-helper`,
		`usr/share/polkit-1/actions/io.semantix.desktop.update.policy`,
	} {
		if !strings.Contains(build, want) {
			t.Errorf("desktop-build.sh missing Linux deb helper contract %q", want)
		}
	}
	for _, unsafe := range []string{
		`dpkg-deb --field "$deb_path" Package | grep -qx`,
		`dpkg-deb --field "$deb_path" Version | grep -qx`,
		`dpkg-deb --field "$deb_path" Depends | grep -Fq`,
		`dpkg-deb --contents "$deb_path" | grep -Eq`,
	} {
		if strings.Contains(build, unsafe) {
			t.Errorf("desktop-build.sh uses early-exit grep under pipefail: %q", unsafe)
		}
	}

	desktopEntry, err := os.ReadFile("build/linux/semantix.desktop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(desktopEntry), "Exec=semantix-launcher") || strings.Contains(string(desktopEntry), "semantix-guard") {
		t.Fatal("Linux desktop entry must launch the permanent launcher without Guard")
	}
	nfpmData, err := os.ReadFile("build/linux/nfpm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	nfpm := string(nfpmData)
	if !strings.Contains(nfpm, "dst: /usr/bin/semantix-launcher") || strings.Contains(nfpm, "dst: /usr/bin/semantix-guard") {
		t.Fatal("Linux deb must install the permanent launcher and must not persist Guard")
	}
	if !strings.Contains(nfpm, "postinstall: ./build/linux/postinstall.sh") {
		t.Fatal("Linux deb must refresh native desktop icon caches after install and upgrade")
	}
	if !strings.Contains(nfpm, "dst: /usr/share/applications/semantix.desktop") {
		t.Fatal("Linux deb must install the Semantix desktop entry")
	}
	postInstall, err := os.ReadFile("build/linux/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gtk-update-icon-cache", "update-desktop-database"} {
		if !strings.Contains(string(postInstall), want) {
			t.Errorf("Linux post-install icon repair missing %q", want)
		}
	}

	windowsData, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(windowsData, []byte{0xef, 0xbb, 0xbf}) {
		t.Fatal("Windows installer script must have a UTF-8 BOM so native makensis accepts localized strings")
	}
	windows := string(windowsData)
	for _, want := range []string{
		`File "/oname=${SEMANTIX_CLI}" "${SEMANTIX_CLI}"`,
		`!define SEMANTIX_UNINST_FINALIZE 'cmd.exe /C copy /Y "%1" "semantix-uninstall.exe" >NUL'`,
		`!uninstfinalize '${SEMANTIX_UNINST_FINALIZE}'`,
		`File "/oname=uninstall.exe" "${ARG_SEMANTIX_SIGNED_UNINSTALLER}"`,
		`StrCpy $R9 "$INSTDIR\versions\.installer-v${INFO_PRODUCTVERSION}-$R8"`,
		`File "/oname=${SEMANTIX_LAYOUT_INSTALLER}" "${SEMANTIX_GUARD}"`,
		`nsExec::ExecToLog /OEM`,
		`Semantix layout activator output:`,
		`--activate-staging "$R9" --no-relaunch`,
		`CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${SEMANTIX_LAUNCHER}" "" "$INSTDIR\${SEMANTIX_LAUNCHER}" 0`,
		`CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${SEMANTIX_LAUNCHER}" "" "$INSTDIR\${SEMANTIX_LAUNCHER}" 0`,
		`StrCmp $SemantixStageMode "1" semantix_stage_payload`,
		`File "/oname=${SEMANTIX_GUARD}" "${SEMANTIX_GUARD}"`,
	} {
		if !strings.Contains(windows, want) {
			t.Errorf("Windows installer missing versioned-layout contract %q", want)
		}
	}
	if strings.Contains(windows, `FileOpen $0 "$INSTDIR\current.json" w`) ||
		strings.Contains(windows, `SetOutPath "$INSTDIR\versions\v${INFO_PRODUCTVERSION}"`) {
		t.Fatal("normal Windows installer must not write the live version or current.json in place")
	}
	if strings.Contains(windows, `ExecWait '"$PLUGINSDIR\${SEMANTIX_LAYOUT_INSTALLER}"`) {
		t.Fatal("Windows installer must not discard layout activator stdout/stderr")
	}
	if strings.Contains(windows, `CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${SEMANTIX_LAUNCHER}" "" "$INSTDIR\versions\v${INFO_PRODUCTVERSION}\${PRODUCT_EXECUTABLE}" 0`) ||
		strings.Contains(windows, `CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${SEMANTIX_LAUNCHER}" "" "$INSTDIR\versions\v${INFO_PRODUCTVERSION}\${PRODUCT_EXECUTABLE}" 0`) {
		t.Fatal("Windows shortcut icon must not point into a version directory that retention removes")
	}
}
