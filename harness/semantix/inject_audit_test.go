package semantix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditInjectionIsOptInAndAppendsCompleteBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit", "instance.txt")
	b := NewBridge(Config{InjectAuditPath: path})

	b.auditInjection("[semantix-reuse]\nfirst\n[/semantix-reuse]")
	b.auditInjection("[semantix-reuse]\nsecond\n[/semantix-reuse]\n")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{"first", "second", "--- semantix injection ---"} {
		if !strings.Contains(got, want) {
			t.Fatalf("audit missing %q:\n%s", want, got)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("audit mode = %v; want 0600", info.Mode().Perm())
	}

	withoutAudit := NewBridge(Config{})
	withoutAudit.auditInjection("secret")
}
