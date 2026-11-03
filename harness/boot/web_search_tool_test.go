package boot

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"semantix/harness/agent/testutil"
	"semantix/harness/event"
)

func TestClientWebSearchToolIsOptInAndCapabilityOnly(t *testing.T) {
	registerBootTokenProfileTestProvider()
	for _, tc := range []struct {
		name     string
		extra    string
		wantWS   bool
		wantWarn string
	}{
		{name: "default", extra: "", wantWS: false},
		{name: "keenable", extra: "\n[tools.web_search]\nprovider = \"keenable\"\n", wantWS: true},
		{name: "enabled-list-without-it", extra: "\n[tools]\nenabled = [\"read_file\"]\n\n[tools.web_search]\nprovider = \"keenable\"\n", wantWS: false},
		{name: "unknown-provider", extra: "\n[tools.web_search]\nprovider = \"bogus\"\n", wantWS: false, wantWarn: `warning: [tools.web_search]: unknown web_search provider "bogus"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			writeFile(t, dir, "semantix-agent.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`+tc.extra)
			prov := testutil.NewMock("web-search-"+tc.name, testutil.Turn{Text: "done"})
			setBootTokenProfileTestProvider(t, prov)
			var stderr bytes.Buffer
			ctrl, err := Build(context.Background(), Options{Sink: event.Discard, Stderr: &stderr})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			if err := ctrl.Run(context.Background(), "hi"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			registered := false
			for _, e := range ctrl.AllToolContractEntries() {
				if e.Name == "web_search" {
					registered = true
				}
			}
			if registered != tc.wantWS {
				t.Fatalf("web_search registered = %v, want %v", registered, tc.wantWS)
			}
			if tc.wantWarn != "" && !strings.Contains(stderr.String(), tc.wantWarn) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.wantWarn)
			}
			// Reached through use_capability only, so it can never collide with
			// a provider-executed web_search tool in the request schema.
			if requestHasTool(prov.Requests()[0], "web_search") {
				t.Fatal("client-side web_search must not be provider-visible")
			}
		})
	}
}
