// Package config loads semantix.toml and merges it with env and CLI flags
// (U20 config layer, minimal set consumed by the U21 `config` command).
//
// Precedence: CLI flag > env (SEMANTIX_*) > semantix.toml > built-in default.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Source identifies which layer supplied a value. U21 requires every printed
// value to be annotated with its origin.
type Source string

const (
	SourceDefault Source = "default"
	SourceFile    Source = "file"
	SourceEnv     Source = "env"
	SourceFlag    Source = "flag"
)

// File mirrors semantix.example.toml. Pointer fields distinguish "absent"
// from the zero value so the merge layer knows which keys the file defined.
type File struct {
	Project   fileProject   `toml:"project"`
	Store     fileStore     `toml:"store"`
	Retrieval fileRetrieval `toml:"retrieval"`
	Inject    fileInject    `toml:"inject"`
	Score     fileScore     `toml:"score"`
	Verify    fileVerify    `toml:"verify"`
	Cost      fileCost      `toml:"cost"`
}

type fileProject struct {
	Name *string `toml:"name"`
}

type fileStore struct {
	DB        *string `toml:"db"`
	Scope     *string `toml:"scope"`
	MaxSlices *int    `toml:"max_slices"`
}

type fileRetrieval struct {
	Retriever *string `toml:"retriever"`
	Limit     *int    `toml:"limit"`
	VectorDim *int    `toml:"vector_dim"`
}

type fileInject struct {
	Budget *int `toml:"budget"`
	TopK   *int `toml:"top_k"`
}

type fileScore struct {
	HalfLifeDays *float64 `toml:"half_life_days"`
	GraceDays    *float64 `toml:"grace_days"`
}

type fileVerify struct {
	Holdout       *float64 `toml:"holdout"`
	TargetHitRate *float64 `toml:"target_hit_rate"`
}

type fileCost struct {
	ToolTokens     *int     `toml:"tool_tokens"`
	ReuseRatio     *float64 `toml:"reuse_ratio"`
	InputPriceUSD  *float64 `toml:"input_price_usd"`
	CachePriceUSD  *float64 `toml:"cache_price_usd"`
	OutputPriceUSD *float64 `toml:"output_price_usd"`
}

// Kind classifies a config error so the CLI can map it to an exit code.
type Kind int

const (
	KindIO      Kind = iota // file missing/unreadable → exit 1
	KindInvalid             // invalid content → exit 2
)

// Error is a config error with an exit-code kind.
type Error struct {
	Kind Kind
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func ioErr(err error) error      { return &Error{Kind: KindIO, Err: err} }
func invalidErr(err error) error { return &Error{Kind: KindInvalid, Err: err} }

// Field is one resolved config entry.
type Field struct {
	Key    string
	Value  any
	Source Source
}

// Resolved is the effective config after flag > env > file > default.
type Resolved struct {
	Fields []Field
}

// Get returns the value and source for a dotted key ("" if absent).
func (r *Resolved) Get(key string) (any, Source, bool) {
	for _, f := range r.Fields {
		if f.Key == key {
			return f.Value, f.Source, true
		}
	}
	return nil, "", false
}

// Options carries explicit CLI overrides (the flag layer). Empty means unset.
type Options struct {
	ConfigPath string // --config; falls back to SEMANTIX_CONFIG env, then ./semantix.toml
	DB         string // --db
}

// Load resolves the effective configuration.
func Load(opts Options) (*Resolved, error) {
	path, explicit := configPath(opts.ConfigPath)

	var file File
	if b, err := os.ReadFile(path); err == nil {
		if _, err := toml.Decode(string(b), &file); err != nil {
			return nil, invalidErr(fmt.Errorf("parse %s: %w", path, err))
		}
	} else if !os.IsNotExist(err) {
		return nil, ioErr(fmt.Errorf("read %s: %w", path, err))
	} else if explicit {
		return nil, ioErr(fmt.Errorf("config file %s does not exist", path))
	}

	r := &Resolved{}
	add := func(f Field, err error) error {
		if err != nil {
			return invalidErr(err)
		}
		r.Fields = append(r.Fields, f)
		return nil
	}

	var dbFlag *string
	if opts.DB != "" {
		dbFlag = &opts.DB
	}

	if err := add(mergePtr("project.name", "my-project", "", file.Project.Name, (*string)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("store.db", ".semantix/project.db", "SEMANTIX_DB", file.Store.DB, dbFlag)); err != nil {
		return nil, err
	}
	if err := add(mergePtr("store.scope", "project", "SEMANTIX_SCOPE", file.Store.Scope, (*string)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("store.max_slices", 5000, "SEMANTIX_MAX_SLICES", file.Store.MaxSlices, (*int)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("retrieval.retriever", "hybrid", "SEMANTIX_RETRIEVER", file.Retrieval.Retriever, (*string)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("retrieval.limit", 5, "SEMANTIX_LIMIT", file.Retrieval.Limit, (*int)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("retrieval.vector_dim", 256, "", file.Retrieval.VectorDim, (*int)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("inject.budget", 4096, "SEMANTIX_INJECT_BUDGET", file.Inject.Budget, (*int)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("inject.top_k", 5, "SEMANTIX_INJECT_TOP_K", file.Inject.TopK, (*int)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("score.half_life_days", 30.0, "", file.Score.HalfLifeDays, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("score.grace_days", 7.0, "", file.Score.GraceDays, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("verify.holdout", 0.3, "SEMANTIX_VERIFY_HOLDOUT", file.Verify.Holdout, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("verify.target_hit_rate", 0.7, "SEMANTIX_VERIFY_TARGET_HIT_RATE", file.Verify.TargetHitRate, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("cost.tool_tokens", 300, "", file.Cost.ToolTokens, (*int)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("cost.reuse_ratio", 0.8, "", file.Cost.ReuseRatio, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("cost.input_price_usd", 0.27, "", file.Cost.InputPriceUSD, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("cost.cache_price_usd", 0.07, "", file.Cost.CachePriceUSD, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("cost.output_price_usd", 1.10, "", file.Cost.OutputPriceUSD, (*float64)(nil))); err != nil {
		return nil, err
	}

	if err := r.validate(); err != nil {
		return nil, invalidErr(err)
	}
	return r, nil
}

// configPath resolves the config file path and whether it was explicitly
// requested (an explicit but missing file is an error, unlike the default).
func configPath(flagPath string) (string, bool) {
	if flagPath != "" {
		return flagPath, true
	}
	if p := os.Getenv("SEMANTIX_CONFIG"); p != "" {
		return p, true
	}
	return "semantix.toml", false
}

// mergePtr resolves one field: flag > env > file > default. A nil pointer
// means the layer did not set the key.
func mergePtr[T any](key string, def T, envName string, filePtr, flagPtr *T) (Field, error) {
	if flagPtr != nil {
		return Field{Key: key, Value: *flagPtr, Source: SourceFlag}, nil
	}
	if envName != "" {
		if s, ok := os.LookupEnv(envName); ok && s != "" {
			v, err := parseEnv(s, def)
			if err != nil {
				return Field{}, fmt.Errorf("%s: invalid %s %q: %w", key, envName, s, err)
			}
			return Field{Key: key, Value: v, Source: SourceEnv}, nil
		}
	}
	if filePtr != nil {
		return Field{Key: key, Value: *filePtr, Source: SourceFile}, nil
	}
	return Field{Key: key, Value: def, Source: SourceDefault}, nil
}

// parseEnv converts an env string to the type of the default value.
func parseEnv(s string, def any) (any, error) {
	switch def.(type) {
	case int:
		v, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("want integer, got %q", s)
		}
		return v, nil
	case float64:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("want number, got %q", s)
		}
		return v, nil
	default:
		return s, nil
	}
}

// validate applies the value-domain rules (U20: invalid config → error with
// field location). Errors are aggregated rather than fail-fast.
func (r *Resolved) validate() error {
	var errs []string
	for _, f := range r.Fields {
		switch f.Key {
		case "store.scope":
			if s, ok := f.Value.(string); ok {
				switch s {
				case "session", "project", "user":
				default:
					errs = append(errs, fmt.Sprintf("store.scope: invalid value %q (want session|project|user)", s))
				}
			}
		case "retrieval.limit":
			if v, ok := f.Value.(int); ok && v <= 0 {
				errs = append(errs, "retrieval.limit: must be > 0")
			}
		case "store.max_slices":
			if v, ok := f.Value.(int); ok && v < 0 {
				errs = append(errs, "store.max_slices: must be >= 0 (0 disables the cap)")
			}
		case "score.half_life_days":
			if v, ok := f.Value.(float64); ok && v <= 0 {
				errs = append(errs, "score.half_life_days: must be > 0")
			}
		case "score.grace_days":
			if v, ok := f.Value.(float64); ok && v < 0 {
				errs = append(errs, "score.grace_days: must be >= 0")
			}
		case "retrieval.retriever":
			if s, ok := f.Value.(string); ok {
				switch s {
				case "bm25", "vector", "hybrid":
				default:
					errs = append(errs, fmt.Sprintf("retrieval.retriever: invalid value %q (want bm25|vector|hybrid)", s))
				}
			}
		case "inject.budget":
			if v, ok := f.Value.(int); ok && v <= 0 {
				errs = append(errs, "inject.budget: must be > 0")
			}
		case "verify.holdout":
			if v, ok := f.Value.(float64); ok && (v < 0 || v >= 1) {
				errs = append(errs, "verify.holdout: must be in [0,1)")
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
