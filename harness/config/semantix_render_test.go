package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestRenderTOMLRoundTripsSemantixAuditDir guards the Issue #326 injection
// audit_dir key: once the semantix section renders, a later config save
// (RenderTOML is used by every setup/migration/editor write) must not drop
// the audit journal location the benchmark runner configured.
func TestRenderTOMLRoundTripsSemantixAuditDir(t *testing.T) {
	cfg := Default()
	cfg.Semantix.Enabled = true
	cfg.Semantix.Inject = true
	cfg.Semantix.SessionsDir = "/run/sessions"
	cfg.Semantix.AuditDir = "/run/audit"

	rendered := RenderTOML(cfg)
	if !strings.Contains(rendered, `audit_dir = "/run/audit"`) {
		t.Fatalf("render missing audit_dir:\n%s", rendered)
	}
	if !strings.Contains(rendered, `sessions_dir = "/run/sessions"`) {
		t.Fatalf("render missing sessions_dir:\n%s", rendered)
	}

	back := Default()
	if _, err := toml.Decode(rendered, back); err != nil {
		t.Fatalf("round-trip decode: %v", err)
	}
	if !back.Semantix.Enabled || !back.Semantix.Inject {
		t.Fatalf("round-trip lost semantix enabled/inject: %+v", back.Semantix)
	}
	if back.Semantix.AuditDir != "/run/audit" || back.Semantix.SessionsDir != "/run/sessions" {
		t.Fatalf("round-trip lost semantix dirs: %+v", back.Semantix)
	}
}
