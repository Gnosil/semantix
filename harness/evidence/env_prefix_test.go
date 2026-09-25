package evidence

import "testing"

func TestEnvPrefixVerification(t *testing.T) {
	cases := map[string]bool{
		"go test ./internal/evidence/":                   true,
		"go test ./internal/evidence/ -count=1":          true,
		"GOROOT=/x go test ./internal/evidence/":         true,
		"GOROOT=/x PATH=/y go test ./internal/evidence/": true,
		"env GOROOT=/x go test ./internal/evidence/":     true,
		"cd /tmp && go test ./internal/evidence/":        true,
		"go test ./internal/evidence/ 2>&1 | tail -3":    false,
		// A verifier on the right of || never runs when the left succeeds,
		// so the overall exit 0 cannot certify it.
		"cat report.txt || go test ./internal/evidence/": false,
		// A verifier on the left of || has its failure swallowed whenever
		// the right side succeeds.
		"go test ./internal/evidence/ || cat report.txt": false,
		"GOROOT=/x rm -rf /":                             false,
		"GOROOT=/x":                                      false,
		"FOO=$(whoami) go test ./internal/evidence/":     false,
		"env -i go test ./internal/evidence/":            false,
		"CARGO_HOME=/y cargo test":                       true,
	}
	for c, want := range cases {
		if got := IsDeliveryVerificationCommand(c); got != want {
			t.Errorf("IsDeliveryVerificationCommand(%q) = %v, want %v", c, got, want)
		}
	}
}
