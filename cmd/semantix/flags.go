package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"semantix/kernel/config"
	"semantix/kernel/evolve"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// zoneFlagSet registers the grey-zone threshold flags (Issue #7) and rebuilds
// a zone.Zones after flag.Parse. All four default to zone.Default().
// Per-type overrides (Issue #259 阶段 2) arrive from semantix.toml only —
// there are no per-type flags; byType stays nil unless the config layer
// supplies it.
type zoneFlagSet struct {
	tauHigh *float64
	tauLow  *float64
	absHigh *float64
	absLow  *float64
	byType  map[string]zone.Zones
}

func addZoneFlags(fs *flag.FlagSet) *zoneFlagSet {
	d := zone.Default()
	return &zoneFlagSet{
		tauHigh: fs.Float64("tau-high", d.TauHigh, "relative confidence for clear hit (score/top1)"),
		tauLow:  fs.Float64("tau-low", d.TauLow, "relative confidence for grey zone"),
		absHigh: fs.Float64("abs-high", d.AbsHigh, "absolute score floor for clear hit"),
		absLow:  fs.Float64("abs-low", d.AbsLow, "absolute score floor for grey zone"),
	}
}

func (zf *zoneFlagSet) zones() zone.Zones {
	z := zone.Zones{
		TauHigh: *zf.tauHigh,
		TauLow:  *zf.tauLow,
		AbsHigh: *zf.absHigh,
		AbsLow:  *zf.absLow,
	}
	if len(zf.byType) > 0 {
		z.ByType = zf.byType
	}
	return z
}

// applyConfigZones lets semantix.toml [retrieval] tau_*/abs_* keys drive
// the grey-zone thresholds (Issue #259 阶段 1). An explicit --tau-*/--abs-*
// flag always wins; config values were validated by the config layer. Call
// between flag.Parse and applyEvolveParams/validate. Effective priority:
// flag > toml > evolve > default.
func (zf *zoneFlagSet) applyConfigZones(fs *flag.FlagSet, c *config.Resolved) error {
	if c == nil {
		return nil
	}
	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		if strings.HasPrefix(f.Name, "tau-") || strings.HasPrefix(f.Name, "abs-") {
			explicit[f.Name] = true
		}
	})
	set := func(name, key string, ptr **float64) {
		if explicit[name] {
			return
		}
		if v, _, ok := c.Get(key); ok {
			if f, ok := v.(float64); ok {
				**ptr = f
			}
		}
	}
	set("tau-high", "retrieval.tau_high", &zf.tauHigh)
	set("tau-low", "retrieval.tau_low", &zf.tauLow)
	set("abs-high", "retrieval.abs_high", &zf.absHigh)
	set("abs-low", "retrieval.abs_low", &zf.absLow)
	// Per-type overrides (Issue #259 阶段 2): assemble the partial toml
	// entries into complete snapshots inheriting the current global
	// thresholds (after the global keys above). Key shape:
	// retrieval.by_type.<type>.<field>; validation already happened in the
	// config layer.
	const pfx = "retrieval.by_type."
	for _, f := range c.Fields {
		if !strings.HasPrefix(f.Key, pfx) {
			continue
		}
		rest := strings.TrimPrefix(f.Key, pfx)
		dot := strings.Index(rest, ".")
		if dot <= 0 {
			continue
		}
		name, field := rest[:dot], rest[dot+1:]
		v, ok := f.Value.(float64)
		if !ok {
			continue
		}
		if zf.byType == nil {
			zf.byType = map[string]zone.Zones{}
		}
		oz, ok := zf.byType[name]
		if !ok {
			oz = zf.zones() // inherit the current global thresholds
			oz.ByType = nil
		}
		switch field {
		case "tau_high":
			oz.TauHigh = v
		case "tau_low":
			oz.TauLow = v
		case "abs_high":
			oz.AbsHigh = v
		case "abs_low":
			oz.AbsLow = v
		}
		zf.byType[name] = oz
	}
	return nil
}

// applyEvolveParams lets the evolved TauL2 drive the grey-zone floor
// (Issue #220: params.json gains its first consumer). An explicit
// --tau-low always wins over the evolved value; the evolved value is
// clamped to the engine's own tuning band so a hand-edited or stale file
// cannot push the classifier outside what evolve itself could produce.
// Call between flag.Parse and validate.
func (zf *zoneFlagSet) applyEvolveParams(fs *flag.FlagSet, dir string) error {
	if dir == "" {
		return nil
	}
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "tau-low" {
			explicit = true
		}
	})
	if explicit {
		return nil
	}
	st, err := loadEvolveState(dir)
	if err != nil || st == nil {
		return err
	}
	tau := st.Params.TauL2
	if math.IsNaN(tau) || math.IsInf(tau, 0) {
		return nil
	}
	if tau < evolve.DefaultMinTau {
		tau = evolve.DefaultMinTau
	}
	if tau > evolve.DefaultMaxTau {
		tau = evolve.DefaultMaxTau
	}
	*zf.tauLow = tau
	return nil
}

// validate checks the parsed thresholds: NaN/Inf/out-of-range values would
// silently distort zone classification (e.g. negative tau -> everything Hit).
// Call after flag.Parse.
func (zf *zoneFlagSet) validate() error {
	if math.IsNaN(*zf.tauHigh) || math.IsInf(*zf.tauHigh, 0) || *zf.tauHigh <= 0 || *zf.tauHigh > 1 {
		return fmt.Errorf("invalid --tau-high %v (want 0 < v <= 1)", *zf.tauHigh)
	}
	if math.IsNaN(*zf.tauLow) || math.IsInf(*zf.tauLow, 0) || *zf.tauLow <= 0 || *zf.tauLow > 1 {
		return fmt.Errorf("invalid --tau-low %v (want 0 < v <= 1)", *zf.tauLow)
	}
	if math.IsNaN(*zf.absHigh) || math.IsInf(*zf.absHigh, 0) || *zf.absHigh < 0 {
		return fmt.Errorf("invalid --abs-high %v (want v >= 0)", *zf.absHigh)
	}
	if math.IsNaN(*zf.absLow) || math.IsInf(*zf.absLow, 0) || *zf.absLow < 0 {
		return fmt.Errorf("invalid --abs-low %v (want v >= 0)", *zf.absLow)
	}
	if *zf.tauHigh <= *zf.tauLow {
		return fmt.Errorf("--tau-high (%v) must be > --tau-low (%v)", *zf.tauHigh, *zf.tauLow)
	}
	return nil
}

func parseScope(value string) (slice.Scope, error) {
	switch value {
	case "session":
		return slice.Session, nil
	case "project":
		return slice.Project, nil
	case "user":
		return slice.User, nil
	default:
		return 0, usagef("invalid scope %q (want session, project, or user)", value)
	}
}

func scopeName(scope slice.Scope) string {
	switch scope {
	case slice.Session:
		return "session"
	case slice.Project:
		return "project"
	case slice.User:
		return "user"
	default:
		return fmt.Sprintf("scope(%d)", scope)
	}
}

func defaultProjectDB() string {
	return filepath.Join(".semantix", "project.db")
}

func defaultUserDB() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".semantix", "user.db")
	}
	return filepath.Join(home, ".semantix", "user.db")
}

func selectDB(scope slice.Scope, override, projectDB, userDB string) string {
	if override != "" {
		return config.ExpandHome(override)
	}
	if scope == slice.User {
		return config.ExpandHome(userDB)
	}
	return config.ExpandHome(projectDB)
}
