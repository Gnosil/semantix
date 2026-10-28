// Package config loads semantix.toml and merges it with env and CLI flags
// (U20 config layer, minimal set consumed by the U21 `config` command).
//
// Precedence: CLI flag > env (SEMANTIX_*) > semantix.toml > built-in default.
package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"semantix/kernel/slice"
	"semantix/kernel/zone"

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
	Slice     fileSlice     `toml:"slice"`
	Inject    fileInject    `toml:"inject"`
	Score     fileScore     `toml:"score"`
	Verify    fileVerify    `toml:"verify"`
	Cost      fileCost      `toml:"cost"`
	Doctor    fileDoctor    `toml:"doctor"`
}

type fileProject struct {
	Name *string `toml:"name"`
}

type fileStore struct {
	DB        *string `toml:"db"`
	Scope     *string `toml:"scope"`
	MaxSlices *int    `toml:"max_slices"`
}

type fileSlice struct {
	MinInjectOrigin *string `toml:"min_inject_origin"`
}

type fileRetrieval struct {
	Retriever  *string  `toml:"retriever"`
	Limit      *int     `toml:"limit"`
	VectorDim  *int     `toml:"vector_dim"`
	Fusion     *string  `toml:"fusion"`      // hybrid 融合策略: weighted | rrf (Issue #274)
	RrfK       *int     `toml:"rrf_k"`       // RRF 常数,默认 60
	BM25Weight *float64 `toml:"bm25_weight"` // weighted 模式 BM25 路权重,默认 0.5
	// Grey-zone 四阈值 (Issue #259 阶段 1): semantix.toml 键,被
	// --tau-*/--abs-* flag 覆盖;值域 (0,1] / >= 0,且 tau_high > tau_low。
	TauHigh *float64 `toml:"tau_high"`
	TauLow  *float64 `toml:"tau_low"`
	AbsHigh *float64 `toml:"abs_high"`
	AbsLow  *float64 `toml:"abs_low"`
	// ByType 是 per-slice-type 阈值覆盖 (Issue #259 阶段 2),key 为
	// slice.SliceType 的稳定 wire 名;平铺为
	// retrieval.by_type.<type>.<field> 键。未知类型名校验失败。
	ByType map[string]fileZone `toml:"by_type"`
}

// fileZone 是 per-type 的部分覆盖语法:未写的字段继承全局阈值。
type fileZone struct {
	TauHigh *float64 `toml:"tau_high"`
	TauLow  *float64 `toml:"tau_low"`
	AbsHigh *float64 `toml:"abs_high"`
	AbsLow  *float64 `toml:"abs_low"`
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
	// Providers holds per-endpoint price overrides (GLM-P0-4, Issue #292):
	// cloud-hosted GLM stacks price identically per model but differ per
	// vendor, so hit-savings must be priced per provider key (the usage
	// log's Event.Provider). Keys flatten to
	// cost.providers.<name>.{input,cache,output}_price_usd.
	Providers map[string]fileProviderCost `toml:"providers"`
}

type fileProviderCost struct {
	InputPriceUSD  *float64 `toml:"input_price_usd"`
	CachePriceUSD  *float64 `toml:"cache_price_usd"`
	OutputPriceUSD *float64 `toml:"output_price_usd"`
}

// fileDoctor configures the optional doctor network checks (GLM-P0-4).
// models_url unset ⇒ the model-catalog check reports SKIP (doctor stays
// fully offline by default).
type fileDoctor struct {
	ModelsURL   *string  `toml:"models_url"`
	WatchModels []string `toml:"watch_models"`
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
	ConfigPath string // --config > SEMANTIX_CONFIG > ./semantix.toml > ~/.semantix/semantix.toml
	DB         string // --db
}

// Load resolves the effective configuration.
func Load(opts Options) (*Resolved, error) {
	path, explicit := ResolvePath(opts.ConfigPath)

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
		if f.Key == "store.db" {
			f.Value = ExpandHome(f.Value.(string))
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
	if err := add(mergePtr("store.db", "", "SEMANTIX_DB", file.Store.DB, dbFlag)); err != nil {
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
	if err := add(mergePtr("retrieval.fusion", "weighted", "", file.Retrieval.Fusion, (*string)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("retrieval.rrf_k", 60, "", file.Retrieval.RrfK, (*int)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("retrieval.bm25_weight", 0.5, "", file.Retrieval.BM25Weight, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("slice.min_inject_origin", "session-auto", "", file.Slice.MinInjectOrigin, (*string)(nil))); err != nil {
		return nil, err
	}
	// Grey-zone 四阈值 (Issue #259 阶段 1): 默认值与 zone.Default() 单真源
	// 对齐;flag > env > file > default 优先级由 mergePtr 统一处理。
	defZ := zone.Default()
	if err := add(mergePtr("retrieval.tau_high", defZ.TauHigh, "", file.Retrieval.TauHigh, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("retrieval.tau_low", defZ.TauLow, "", file.Retrieval.TauLow, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("retrieval.abs_high", defZ.AbsHigh, "", file.Retrieval.AbsHigh, (*float64)(nil))); err != nil {
		return nil, err
	}
	if err := add(mergePtr("retrieval.abs_low", defZ.AbsLow, "", file.Retrieval.AbsLow, (*float64)(nil))); err != nil {
		return nil, err
	}

	// Per-type 覆盖 (Issue #259 阶段 2): 平铺为 dotted 键,按类型名排序
	// 保证 Fields 确定性 (config list 输出与测试)。
	typeNames := make([]string, 0, len(file.Retrieval.ByType))
	for name := range file.Retrieval.ByType {
		typeNames = append(typeNames, name)
	}
	sort.Strings(typeNames)
	for _, name := range typeNames {
		fz := file.Retrieval.ByType[name]
		prefix := "retrieval.by_type." + name + "."
		for _, kv := range []struct {
			field string
			ptr   *float64
		}{
			{"tau_high", fz.TauHigh},
			{"tau_low", fz.TauLow},
			{"abs_high", fz.AbsHigh},
			{"abs_low", fz.AbsLow},
		} {
			if kv.ptr != nil {
				r.Fields = append(r.Fields, Field{Key: prefix + kv.field, Value: *kv.ptr, Source: SourceFile})
			}
		}
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

	// Per-provider price overrides flatten to dotted keys in sorted-name
	// order so Fields stays deterministic (config list output, tests). Only
	// present sub-keys are added: absent prices fall back to the global
	// cost.* figures at the consumer.
	providerNames := make([]string, 0, len(file.Cost.Providers))
	for name := range file.Cost.Providers {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		pc := file.Cost.Providers[name]
		prefix := "cost.providers." + name + "."
		for key, ptr := range map[string]*float64{
			"input_price_usd":  pc.InputPriceUSD,
			"cache_price_usd":  pc.CachePriceUSD,
			"output_price_usd": pc.OutputPriceUSD,
		} {
			if ptr != nil {
				r.Fields = append(r.Fields, Field{Key: prefix + key, Value: *ptr, Source: SourceFile})
			}
		}
	}
	// Map-range order is random; re-sort the provider block for determinism.
	sort.SliceStable(r.Fields, func(i, j int) bool {
		pi := strings.HasPrefix(r.Fields[i].Key, "cost.providers.")
		pj := strings.HasPrefix(r.Fields[j].Key, "cost.providers.")
		if pi != pj {
			return !pi // non-provider fields keep their declaration order first
		}
		if !pi {
			return false // stable: leave non-provider relative order alone
		}
		return r.Fields[i].Key < r.Fields[j].Key
	})

	if err := add(mergePtr("doctor.models_url", "", "SEMANTIX_DOCTOR_MODELS_URL", file.Doctor.ModelsURL, (*string)(nil))); err != nil {
		return nil, err
	}
	watch := strings.Join(file.Doctor.WatchModels, ",")
	r.Fields = append(r.Fields, Field{Key: "doctor.watch_models", Value: watch, Source: sourceOf(len(file.Doctor.WatchModels) > 0)})

	if err := r.validate(); err != nil {
		return nil, invalidErr(err)
	}
	return r, nil
}

// sourceOf maps "was it set in the file" to the Field source label.
func sourceOf(fromFile bool) Source {
	if fromFile {
		return SourceFile
	}
	return SourceDefault
}

// ResolvePath resolves the config file path and whether it was explicitly
// requested (an explicit but missing file is an error, unlike the default).
func ResolvePath(flagPath string) (string, bool) {
	if flagPath != "" {
		return ExpandHome(flagPath), true
	}
	if p := os.Getenv("SEMANTIX_CONFIG"); p != "" {
		return ExpandHome(p), true
	}
	const local = "semantix.toml"
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		return local, false
	}
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, ".semantix", local)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return path, false
		}
	}
	return local, false
}

// ExpandHome expands only a leading ~ or ~/ (also ~\ on Windows).
// Other relative paths retain their existing working-directory semantics.
func ExpandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
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
		case "retrieval.fusion":
			if s, ok := f.Value.(string); ok {
				switch s {
				case "weighted", "rrf":
				default:
					errs = append(errs, fmt.Sprintf("retrieval.fusion: invalid value %q (want weighted|rrf)", s))
				}
			}
		case "retrieval.rrf_k":
			if v, ok := f.Value.(int); ok && v <= 0 {
				errs = append(errs, "retrieval.rrf_k: must be > 0")
			}
		case "retrieval.bm25_weight":
			if v, ok := f.Value.(float64); ok && (v < 0 || v > 1) {
				errs = append(errs, "retrieval.bm25_weight: must be in [0,1]")
			}
		case "slice.min_inject_origin":
			if s, ok := f.Value.(string); ok {
				switch s {
				case "import", "session-auto", "prefetch", "user-curated":
				default:
					errs = append(errs, fmt.Sprintf("slice.min_inject_origin: invalid value %q (want import|session-auto|prefetch|user-curated)", s))
				}
			}
		case "retrieval.tau_high", "retrieval.tau_low":
			if v, ok := f.Value.(float64); ok && (math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > 1) {
				errs = append(errs, fmt.Sprintf("%s: must be in (0,1]", f.Key))
			}
		case "retrieval.abs_high", "retrieval.abs_low":
			if v, ok := f.Value.(float64); ok && (math.IsNaN(v) || math.IsInf(v, 0) || v < 0) {
				errs = append(errs, fmt.Sprintf("%s: must be >= 0", f.Key))
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
	// Per-type 覆盖 (Issue #259 阶段 2): 键形如 retrieval.by_type.<type>.<field>。
	// 类型名必须合法 (fail-closed 防拼写错误),值域与全局同规则,且
	// 同类型内 tau_high > tau_low。
	byTypeTau := map[string]struct{ high, low float64 }{} // 交叉校验收集
	byTypeSeen := map[string]bool{}
	for _, f := range r.Fields {
		const pfx = "retrieval.by_type."
		if !strings.HasPrefix(f.Key, pfx) {
			continue
		}
		rest := strings.TrimPrefix(f.Key, pfx)
		dot := strings.Index(rest, ".")
		if dot <= 0 {
			errs = append(errs, fmt.Sprintf("%s: malformed per-type key", f.Key))
			continue
		}
		name, field := rest[:dot], rest[dot+1:]
		if _, ok := slice.TypeFromString(name); !ok {
			errs = append(errs, fmt.Sprintf("%s: %q is not a slice type (want prompt|context|tool_pattern|result|memory)", f.Key, name))
			continue
		}
		v, ok := f.Value.(float64)
		if !ok {
			continue
		}
		switch field {
		case "tau_high", "tau_low":
			if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > 1 {
				errs = append(errs, fmt.Sprintf("%s: must be in (0,1]", f.Key))
			}
		case "abs_high", "abs_low":
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				errs = append(errs, fmt.Sprintf("%s: must be >= 0", f.Key))
			}
		default:
			errs = append(errs, fmt.Sprintf("%s: unknown threshold field", f.Key))
			continue
		}
		e := byTypeTau[name]
		if field == "tau_high" {
			e.high = v
		}
		if field == "tau_low" {
			e.low = v
		}
		byTypeTau[name] = e
		byTypeSeen[name] = true
	}
	for name := range byTypeSeen {
		e := byTypeTau[name]
		if e.high != 0 && e.low != 0 && e.high <= e.low {
			errs = append(errs, fmt.Sprintf("retrieval.by_type.%s.tau_high (%v) must be > tau_low (%v)", name, e.high, e.low))
		}
	}
	// Cross-key invariant (Issue #259 阶段 1): the clear-hit floor must sit
	// above the grey floor, otherwise the classifier degenerates.
	if th, _, ok := r.Get("retrieval.tau_high"); ok {
		if tl, _, ok2 := r.Get("retrieval.tau_low"); ok2 {
			thv, thOK := th.(float64)
			tlv, tlOK := tl.(float64)
			if thOK && tlOK && thv <= tlv {
				errs = append(errs, fmt.Sprintf("retrieval.tau_high (%v) must be > retrieval.tau_low (%v)", thv, tlv))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
