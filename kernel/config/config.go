// Package config loads semantix.toml with the M2-U20 priority chain:
// CLI flag > env > semantix.toml > built-in default.
//
// The zero-dependency stance is waived for configuration parsing only:
// TOML's edge cases (escapes, duplicate keys, positions) are exactly where a
// hand-rolled parser hides bugs, and a misparse blocks every command. See
// Issue #114.
package config

import (
	"bytes"
	"fmt"
	"os"

	toml "github.com/pelletier/go-toml/v2"
)

// Config is the full shape of semantix.toml. Fields without a current
// consumer are still parsed and validated so the file remains forward-looking;
// they are documented "reserved" until a later issue wires them.
type Config struct {
	Project   Project   `toml:"project"`
	Store     Store     `toml:"store"`
	Retrieval Retrieval `toml:"retrieval"`
	Inject    Inject    `toml:"inject"`
	Verify    Verify    `toml:"verify"`
	Cost      Cost      `toml:"cost"`
}

type Project struct {
	Name string `toml:"name"`
}

type Store struct {
	DB    string `toml:"db"`
	Scope string `toml:"scope"`
}

type Retrieval struct {
	Retriever   string `toml:"retriever"`
	SearchLimit int    `toml:"search_limit"`
	LookupLimit int    `toml:"lookup_limit"`
	VectorDim   int    `toml:"vector_dim"` // reserved: validated, not yet consumed
}

type Inject struct {
	Budget int `toml:"budget"`
	TopK   int `toml:"top_k"`
}

type Verify struct {
	Holdout       float64 `toml:"holdout"`
	TargetHitRate float64 `toml:"target_hit_rate"` // reserved: no consumer yet
}

type Cost struct {
	ToolTokens     int     `toml:"tool_tokens"`      // reserved
	ReuseRatio     float64 `toml:"reuse_ratio"`      // reserved
	InputPriceUSD  float64 `toml:"input_price_usd"`
	CachePriceUSD  float64 `toml:"cache_price_usd"`
	OutputPriceUSD float64 `toml:"output_price_usd"` // reserved
}

// Default returns the built-in defaults. These MUST match the hard-coded
// values in cmd/semantix so that a machine with no semantix.toml behaves
// exactly as before this change.
func Default() Config {
	return Config{
		Store: Store{
			DB:    ".semantix/project.db",
			Scope: "project",
		},
		Retrieval: Retrieval{
			Retriever:   "bm25",
			SearchLimit: 10,
			LookupLimit: 5,
			VectorDim:   256,
		},
		Inject: Inject{
			Budget: 4096,
			TopK:   5,
		},
		Verify: Verify{
			Holdout:       0.3,
			TargetHitRate: 0.7,
		},
		Cost: Cost{
			ToolTokens:     300,
			ReuseRatio:     0.8,
			InputPriceUSD:  0.27,
			CachePriceUSD:  0.07,
			OutputPriceUSD: 1.10,
		},
	}
}

// Source records where each top-level field's effective value came from, for
// `semantix config` output (U21) and for diagnosing "config not taking
// effect". Keys are dotted paths like "store.db".
type Source map[string]string

// Origin values for Source.
const (
	OriginDefault = "default"
	OriginFile    = "file"
	OriginEnv     = "env"
)

// LoadOptions controls Load.
type LoadOptions struct {
	// Path is the config file to read. When empty, the default ./semantix.toml
	// is used. Explicit=true means Path was user-specified (--config or
	// SEMANTIX_CONFIG): a missing file is then an error, whereas a missing
	// implicit default file is silently skipped.
	Path     string
	Explicit bool
	Getenv   func(string) string
}

// Error is a configuration error carrying field and position information so
// the CLI can emit exit code 2 with a precise location.
type Error struct {
	Path   string // config file path, or "" for env/default-origin errors
	Field  string // dotted field path, e.g. "retrieval.search_limit"
	Line   int    // 1-based; 0 when not file-origin
	Column int
	Msg    string
}

func (e *Error) Error() string {
	loc := ""
	if e.Path != "" {
		loc = e.Path
		if e.Line > 0 {
			loc += fmt.Sprintf(":%d", e.Line)
			if e.Column > 0 {
				loc += fmt.Sprintf(":%d", e.Column)
			}
		}
		loc += ": "
	}
	if e.Field != "" {
		return fmt.Sprintf("config: %s[%s]: %s", loc, e.Field, e.Msg)
	}
	return fmt.Sprintf("config: %s%s", loc, e.Msg)
}

// Load builds the effective config. Returns the merged Config and the Source
// map for the fields that were set by file or env. A validation or parse
// failure returns *Error.
func Load(opts LoadOptions) (*Config, Source, error) {
	cfg := Default()
	src := Source{}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}

	if opts.Explicit && opts.Path == "" {
		return nil, nil, &Error{Msg: "config path is empty"}
	}

	path := opts.Path
	if path == "" {
		path = "semantix.toml"
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := decodeInto(data, &cfg, src, path); err != nil {
			return nil, nil, err
		}
	case os.IsNotExist(err) && !opts.Explicit:
		// Implicit default missing -> built-ins only, no error.
	case os.IsNotExist(err) && opts.Explicit:
		return nil, nil, &Error{Path: path, Msg: "file not found"}
	default:
		return nil, nil, &Error{Path: path, Msg: err.Error()}
	}

	// env layer: SEMANTIX_DB overrides store.db.
	if v := opts.Getenv("SEMANTIX_DB"); v != "" {
		cfg.Store.DB = v
		src["store.db"] = OriginEnv
	}

	if err := cfg.validate(); err != nil {
		return nil, nil, err
	}
	return &cfg, src, nil
}

func decodeInto(data []byte, cfg *Config, src Source, path string) error {
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		line, col := 0, 0
		var de *toml.DecodeError
		if asDecodeErr(err, &de) {
			line, col = de.Position()
		}
		return &Error{Path: path, Line: line, Column: col, Msg: err.Error()}
	}
	// Mark file-origin fields (the ones this issue consumes, plus the
	// reserved-but-parsed ones, so `semantix config` can report them all).
	for _, f := range []string{"store.db", "store.scope", "retrieval.retriever", "retrieval.search_limit", "retrieval.lookup_limit", "retrieval.vector_dim", "inject.budget", "inject.top_k", "verify.holdout", "verify.target_hit_rate", "cost.input_price_usd", "cost.cache_price_usd", "project.name"} {
		src[f] = OriginFile
	}
	return nil
}

// validate checks enum fields and numeric ranges. Zero values are tolerated
// (they mean "unset in file"); command layers apply their own >0 rules.
func (c *Config) validate() error {
	if c.Store.Scope != "" && !oneOf(c.Store.Scope, "session", "project", "user") {
		return &Error{Field: "store.scope", Msg: fmt.Sprintf("invalid scope %q (want session, project, or user)", c.Store.Scope)}
	}
	if c.Retrieval.Retriever != "" && !oneOf(c.Retrieval.Retriever, "bm25", "vector", "hybrid") {
		return &Error{Field: "retrieval.retriever", Msg: fmt.Sprintf("invalid retriever %q (want bm25, vector, or hybrid)", c.Retrieval.Retriever)}
	}
	for field, v := range map[string]int{
		"retrieval.search_limit": c.Retrieval.SearchLimit,
		"retrieval.lookup_limit": c.Retrieval.LookupLimit,
		"retrieval.vector_dim":   c.Retrieval.VectorDim,
		"inject.budget":          c.Inject.Budget,
		"inject.top_k":           c.Inject.TopK,
	} {
		if v < 0 {
			return &Error{Field: field, Msg: fmt.Sprintf("must be >= 0, got %d", v)}
		}
	}
	if c.Verify.Holdout < 0 || c.Verify.Holdout > 1 {
		return &Error{Field: "verify.holdout", Msg: fmt.Sprintf("must be in [0,1], got %v", c.Verify.Holdout)}
	}
	for field, v := range map[string]float64{
		"cost.input_price_usd":  c.Cost.InputPriceUSD,
		"cost.cache_price_usd":  c.Cost.CachePriceUSD,
		"cost.output_price_usd": c.Cost.OutputPriceUSD,
	} {
		if v < 0 {
			return &Error{Field: field, Msg: fmt.Sprintf("must be >= 0, got %v", v)}
		}
	}
	return nil
}

func oneOf(v string, want ...string) bool {
	for _, w := range want {
		if v == w {
			return true
		}
	}
	return false
}

// asDecodeErr is a tiny indirection to keep the toml.DecodeError type import
// local and testable.
func asDecodeErr(err error, target **toml.DecodeError) bool {
	de, ok := err.(*toml.DecodeError)
	if ok {
		*target = de
	}
	return ok
}

// IsError reports whether err is a *Error and returns it unwrapped.
func IsError(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	if ce, ok := err.(*Error); ok {
		return ce, true
	}
	return nil, false
}
