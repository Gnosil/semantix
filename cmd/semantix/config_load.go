package main

import (
	"os"
	"strings"

	"semantix/kernel/config"
)

// explicitConfigPath extracts the config file path from args (--config x or
// --config=x) or SEMANTIX_CONFIG. It mirrors the flag package's parse rule:
// scanning stops at the first positional argument or "--", and skips the value
// token of value-taking flags, so a query text that happens to contain
// "--config=..." cannot redirect the config file, while `--input x --config y`
// (flag value before --config) still works.
//
// The second return value reports whether the path was explicitly specified
// (flag or env). A bare "--config" with no value returns ("", true): the
// caller must treat it as an explicit-but-empty path, not a silent fallback.
func explicitConfigPath(args []string, getenv func(string) string) (path string, explicit bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if a == "--config" {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		if strings.HasPrefix(a, "--config=") {
			return strings.TrimPrefix(a, "--config="), true
		}
		// Value-taking flags consume the next token as their value. Boolean
		// flags do not; skipping their "value" would swallow the next real
		// token (usually --config or a positional). Inline values
		// (--flag=value) embed their value and consume nothing.
		if strings.HasPrefix(a, "--") && !boolFlag(a) && !strings.Contains(a, "=") {
			i++ // skip the flag's value token
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			continue // boolean flag or short flag; no value token
		}
		// First positional argument: flag parsing stops here.
		break
	}
	if v := getenv("SEMANTIX_CONFIG"); v != "" {
		return v, true
	}
	return "", false
}

// boolFlag reports flags that take no value. Must stay in sync with the
// boolean flags registered by the subcommands that call explicitConfigPath.
func boolFlag(a string) bool {
	switch a {
	case "--json", "--l3-safe", "--strict", "--help", "-h":
		return true
	}
	return false
}

// loadConfigFor loads the effective config. explicit distinguishes an
// absent config selector (implicit ./semantix.toml, missing file is silent)
// from a user-specified one (missing or empty is a config error).
func loadConfigFor(path string, explicit bool, getenv func(string) string) (*config.Config, error) {
	cfg, _, err := config.Load(config.LoadOptions{Path: path, Explicit: explicit, Getenv: getenv})
	return cfg, err
}

// defaultGetenv is os.Getenv; kept as a variable for test seams.
var defaultGetenv = os.Getenv
