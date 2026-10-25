package main

import (
	"flag"

	"semantix/kernel/config"
	"semantix/kernel/slice"
)

// Explicit path flags beat a pinned store.db; an unset store.db leaves
// selectDB's scope defaults intact.
func selectConfiguredDB(flags *flag.FlagSet, cfg *config.Resolved, scope slice.Scope, override, projectDB, userDB string) string {
	scopeFlag := "project-db"
	if scope == slice.User {
		scopeFlag = "user-db"
	}
	explicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "db" || f.Name == scopeFlag {
			explicit = true
		}
	})
	if override == "" && !explicit {
		override = cfgString(cfg, "store.db", "")
	}
	return selectDB(scope, override, projectDB, userDB)
}

// cfgString / cfgInt / cfgFloat read a resolved config entry (M2-U20) with
// a fallback for when the config layer is absent (nil) or the key is unset.
// The resolved value already encodes flag > env > file > default precedence;
// CLI flags override it naturally because flag defaults are only consulted
// when the caller does not pass the flag explicitly.

func cfgString(c *config.Resolved, key, fallback string) string {
	if c == nil {
		return fallback
	}
	if v, _, ok := c.Get(key); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

func cfgInt(c *config.Resolved, key string, fallback int) int {
	if c == nil {
		return fallback
	}
	if v, _, ok := c.Get(key); ok {
		if n, ok := v.(int); ok && n > 0 {
			return n
		}
	}
	return fallback
}

func cfgFloat(c *config.Resolved, key string, fallback float64) float64 {
	if c == nil {
		return fallback
	}
	if v, _, ok := c.Get(key); ok {
		if f, ok := v.(float64); ok && f > 0 {
			return f
		}
	}
	return fallback
}
