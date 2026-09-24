package slice

import "semantix/kernel/fingerprint"

// SliceType classifies a semantic slice (see architecture spec §3.1).
type SliceType int

const (
	// Prompt slice: reusable task templates and instruction blocks.
	Prompt SliceType = iota
	// Context slice: project structure summaries, frequent files, preferences.
	Context
	// ToolPattern slice: tool-call sequences (behavior patterns).
	ToolPattern
	// Result slice: high-frequency tool results / answers.
	Result
	// Memory slice: semantic units linked to the memory system.
	Memory
)

// Scope bounds where a slice is visible.
type Scope int

const (
	// Session scope: one session only.
	Session Scope = iota
	// Project scope: current workspace.
	Project
	// User scope: across projects for this user.
	User
)

// String returns the stable wire name of a SliceType.
func (t SliceType) String() string {
	switch t {
	case Prompt:
		return "prompt"
	case Context:
		return "context"
	case ToolPattern:
		return "tool_pattern"
	case Result:
		return "result"
	case Memory:
		return "memory"
	}
	return "unknown"
}

// TypeFromString resolves a wire name back to a SliceType (Issue #259
// 阶段 2 per-type configuration); ok=false for unknown names so config
// layers can fail closed instead of silently accepting a typo.
func TypeFromString(s string) (SliceType, bool) {
	switch s {
	case "prompt":
		return Prompt, true
	case "context":
		return Context, true
	case "tool_pattern":
		return ToolPattern, true
	case "result":
		return Result, true
	case "memory":
		return Memory, true
	}
	return 0, false
}

// EvictPriorityOf returns the type's eviction priority: lower values are
// evicted first when the library cap overflows. The v1 table is fixed and
// deterministic (Issue #277 typed context eviction): result/tool_pattern go
// stale fastest, prompt/context are project knowledge worth keeping, memory
// sits in between, and unknown types take the most conservative slot so a
// future type is never prioritized for removal.
func EvictPriorityOf(t SliceType) int {
	switch t {
	case Result:
		return 0
	case ToolPattern:
		return 1
	case Memory:
		return 2
	case Prompt:
		return 3
	case Context:
		return 4
	}
	return 5
}

// String returns the stable wire name of a Scope.
func (s Scope) String() string {
	switch s {
	case Session:
		return "session"
	case Project:
		return "project"
	case User:
		return "user"
	}
	return "unknown"
}

// SliceStats tracks usage feedback used by the evolution engine.
type SliceStats struct {
	Hits         uint64
	Misses       uint64
	Injected     uint64
	Rejected     uint64
	Useful       uint64
	Neutral      uint64
	Harmful      uint64
	UserFeedback float64 // +1 keep / -1 reject / 0 none
	// LastUsed is the unix-seconds time this slice last served a hit or an
	// injection. 0 = never used (or legacy line without the field). Unlike
	// the counters it merges by max, not by accumulation — see mergeStats.
	LastUsed int64 `json:"last_used,omitempty"`
}

// ResultStatus records whether a Result slice is still an untrusted final
// answer or has host-observable success evidence. Empty legacy metadata is
// deliberately interpreted as probation.
type ResultStatus string

const (
	ResultStatusProbation ResultStatus = "probation"
	ResultStatusVerified  ResultStatus = "verified"
)

// mergeStats folds delta into cur. Counters accumulate; LastUsed max-merges.
// Live UpdateStats and journal replay share this single rule — if they ever
// disagreed, replaying a journal would compute different stats than the
// process that wrote it.
func mergeStats(cur *SliceStats, delta SliceStats) {
	cur.Hits += delta.Hits
	cur.Misses += delta.Misses
	cur.Injected += delta.Injected
	cur.Rejected += delta.Rejected
	cur.Useful += delta.Useful
	cur.Neutral += delta.Neutral
	cur.Harmful += delta.Harmful
	cur.UserFeedback += delta.UserFeedback
	if delta.LastUsed > cur.LastUsed {
		cur.LastUsed = delta.LastUsed
	}
}

// SliceMeta records provenance.
type SliceMeta struct {
	SourceSession string
	TaskType      string
	Language      string
	ProjectSlug   string
	// BaseCommit is the repository revision visible when the source session ran.
	BaseCommit string `json:"base_commit,omitempty"`
	// Origin is the provenance/trust tag (Issue #279): writing channels
	// stamp it, injection and the L3 gate check its integrity level.
	// Empty means unlabelled (legacy) — treated as the lowest level
	// (fail-closed: no injection, no L3 Hit pass-through). trust upgrades
	// it; see Origin.Level.
	Origin Origin `json:"origin,omitempty"`
	// CompressionVersion identifies the deterministic extraction rules applied
	// before Content and its hash-derived ID were produced. Empty means a
	// legacy or generated slice that did not pass through source compression.
	CompressionVersion string `json:"compression_version,omitempty"`
	// SanitizeVersion records the kernel/sanitize rule revision applied to
	// Content at ingestion (Issue #278): every extracted slice passes the
	// deterministic sanitization pipeline (escape stripping + injection
	// feature removal + sensitive redaction) before its ID is derived.
	// Empty means a legacy slice that predates the write-side pipeline —
	// the inject side re-sanitizes idempotently as a backstop.
	SanitizeVersion string `json:"sanitize_version,omitempty"`
	// OriginalBytes and StoredBytes make extraction compression observable
	// without mixing non-LLM work into the model usage ledger.
	OriginalBytes int `json:"original_bytes,omitempty"`
	StoredBytes   int `json:"stored_bytes,omitempty"`
	// Deps captures the dependency fingerprint at slice time (path -> sha256,
	// Issue #8): reuse is gated on these files not having changed.
	Deps fingerprint.Deps `json:"deps,omitempty"`
	// Mtimes captures file modification times at slice time (path -> unix
	// seconds, U16): cheap fast-fail check before the sha256 re-read.
	Mtimes map[string]int64 `json:"mtimes,omitempty"`
	// L3Safe marks a dependency-free Result slice as explicitly reusable at
	// the L3 gate (opt-in via extract --l3-safe; U16 MEDIUM fix). Slices
	// with captured Deps are inherently safe — this flag is only consulted
	// when Deps is empty, so a shared/injected library cannot silently
	// mark results reusable.
	L3Safe bool `json:"l3_safe,omitempty"`
	// EmbedModel / EmbedDim record the provenance of Slice.Embedding
	// (Issue #63): which embedder produced the vector and its dimension, so
	// future retrieval can detect mixed-dimension libraries instead of
	// silently skipping dimension-mismatched vectors.
	EmbedModel string `json:"embed_model,omitempty"`
	EmbedDim   int    `json:"embed_dim,omitempty"`
	// ContextHash records the messages-context fingerprint of the request
	// that produced this slice (Issue #133 gateway). The L3 gate compares
	// it against the live request so the same query under a different
	// conversation history never reuses another session's outcome. Empty
	// means unknown/legacy — the L3 gate skips the check then.
	ContextHash string `json:"context_hash,omitempty"`
	// Model records the client-visible model alias that produced this
	// slice (Issue #133 gateway). Cross-model reuse is never allowed, so
	// the L3 gate requires a match when non-empty.
	Model string `json:"model,omitempty"`
	// ResultStatus gates Result slices at L2. Automatic extraction starts them
	// in probation and only records verified when a successful verification
	// follows the latest workspace mutation in the transcript.
	ResultStatus               ResultStatus `json:"result_status,omitempty"`
	ResultVerifiedBy           string       `json:"result_verified_by,omitempty"`
	ResultVerificationEvidence string       `json:"result_verification_evidence,omitempty"`
}

// EffectiveResultStatus fails closed for legacy, empty, and unknown values.
func (m SliceMeta) EffectiveResultStatus() ResultStatus {
	if m.ResultStatus == ResultStatusVerified {
		return ResultStatusVerified
	}
	return ResultStatusProbation
}

// Slice is the core semantic slice value.
type Slice struct {
	ID      string // stable UUID; content hash (sha256) is the version field
	Type    SliceType
	Scope   Scope
	Content []byte // normalized text (Prompt/Context/Result/Memory) or tool sequence (ToolPattern)
	// Embedding is persisted only for model-produced vectors (hash vectors
	// are recomputable from Content and are not stored). Store reads return
	// nil by contract — nothing in the repo consumes persisted vectors; the
	// raw bytes still round-trip through Export and compaction.
	Embedding []float32 `json:",omitempty"`
	Stats     SliceStats
	Weight    float64 // value weight, updated by the evolution engine
	Meta      SliceMeta
	// CreatedAt is the unix-seconds creation time (maintenance gc retention
	// basis). Zero means unknown (legacy/imported lines without the field):
	// retention never expires unknown-age slices.
	CreatedAt int64 `json:"created_at,omitempty"`
}

// Hit is one search result.
type Hit struct {
	Slice *Slice
	Score float64
	// Lexical is the lexical-support score in [0,1] contributed by the
	// lexical (BM25) route of a fused retrieval: 0 means the candidate was
	// a pure-vector hit with no term overlap (Issue #260 lexical support
	// gate). In single-route modes it carries the query-token coverage
	// (vector mode) or 1 (bm25 mode). LexicalValid=false (zero value)
	// means the index did not evaluate lexical support — consumers must
	// treat that as "not measured", not "unsupported", so legacy and
	// third-party Index implementations are never blocked by default.
	Lexical      float64 `json:"lexical,omitempty"`
	LexicalValid bool    `json:"-"`
}

// Origin is the provenance/trust tag of a slice (Issue #279): writing
// channels stamp it, and injection / the L3 gate check its integrity
// level. The empty string means unlabelled (legacy) and is treated as
// the lowest level — fail-closed (never injected, never passed through
// the L3 Hit gate) unless explicitly trusted.
type Origin string

const (
	// OriginSessionAuto: automatically extracted from a session transcript
	// (ingest pipeline, gateway ingestion).
	OriginSessionAuto Origin = "session-auto"
	// OriginPrefetch: produced speculatively by the prefetch runner.
	OriginPrefetch Origin = "prefetch"
	// OriginImport: loaded from an external file — the most open channel,
	// never trusted by default.
	OriginImport Origin = "import"
	// OriginUserCurated: explicitly curated by the user (extract CLI,
	// trust upgrade, import --trust).
	OriginUserCurated Origin = "user-curated"
)

// Level maps an Origin to its integrity level (Issue #279 §3.1):
// import and unlabelled (legacy) → 1, session-auto and prefetch → 2,
// user-curated → 3. A configured floor above a slice's level excludes it
// from injection and downgrades its L3 Hit to judge-gated Grey.
func (o Origin) Level() int {
	switch o {
	case OriginUserCurated:
		return 3
	case OriginSessionAuto, OriginPrefetch:
		return 2
	default: // "" (legacy) and OriginImport
		return 1
	}
}

// Valid reports whether o is a known non-empty origin tag.
func (o Origin) Valid() bool {
	switch o {
	case OriginSessionAuto, OriginPrefetch, OriginImport, OriginUserCurated:
		return true
	}
	return false
}
