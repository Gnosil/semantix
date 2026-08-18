package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"semantix/kernel/config"
	"semantix/kernel/slice"
	"semantix/kernel/usage"
	"semantix/kernel/zone"
)

// runDashboard renders a one-screen ANSI snapshot dashboard (U31, Issue #155):
// the reuse value of the semantic cache library at a glance, using only the
// standard library — ANSI escapes + Unicode block characters, no bubbletea.
//
// Blocks:
//
//	💰 Cost savings     — SavingsUSD + savings-rate bar, from the usage log
//	🎯 Cache hit rate   — (L3 + L2 hits) / total turns, from the usage log
//	🗂 Zone distribution — hit/grey/miss bars; verify replays are not
//	                       persisted, so the dashboard recomputes the zone
//	                       distribution by replaying the slice library with
//	                       the exact thresholds verify uses (classifyTop1 +
//	                       zone.Default(), excluding the query's own slice)
//	📦 Slice library     — total slices + distinct SourceSession count
//
// Data sources are read-only (kernel/usage, kernel/zone, kernel/slice):
//   - usage log: default .semantix/usage.jsonl (override --usage)
//   - slice library: default from U20 config store.db → .semantix/project.db
//
// The dashboard never creates a store: a missing library renders zeros.
// Missing data files fail open (zeros), matching the repo's Fail Open design
// principle; usage errors (bad flags, invalid config) exit 2 per U19 §4.3 and
// config file IO errors exit 1.
func runDashboard(args []string, stdout, stderr io.Writer, deps dependencies) int {
	fs := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "config file path (default ./semantix.toml)")
	dbOverride := fs.String("db", "", "slice library path override (default: U20 config store.db)")
	usagePath := fs.String("usage", filepath.Join(".semantix", "usage.jsonl"), "usage log path (default .semantix/usage.jsonl)")
	jsonOut := fs.Bool("json", false, "write JSON envelope output")
	noColor := fs.Bool("no-color", false, "disable ANSI colors")
	width := fs.Int("width", 24, "bar width in characters (1-64)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "dashboard: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *width < 1 || *width > 64 {
		fmt.Fprintf(stderr, "dashboard: --width must be in [1,64] (got %d)\n", *width)
		return 2
	}

	// U20 config defaults: --db falls back to store.db with
	// flag > env > file > built-in precedence.
	res, err := config.Load(config.Options{ConfigPath: *configPath, DB: *dbOverride})
	if err != nil {
		code := 2
		var ce *config.Error
		if errors.As(err, &ce) && ce.Kind == config.KindIO {
			code = 1
		}
		if *jsonOut {
			_ = writeErrorEnvelope(stdout, "dashboard", code, err.Error())
		}
		fmt.Fprintf(stderr, "dashboard: %v\n", err)
		return code
	}
	db := *dbOverride
	if db == "" {
		if v, _, ok := res.Get("store.db"); ok {
			if s, ok := v.(string); ok {
				db = s
			}
		}
	}

	data := dashboardPayload{}

	// 💰 + 🎯 from the usage log (read-only; missing → zeros, fail open).
	s, uerr := usage.Summarize(*usagePath, usage.DefaultCostMissPerMTok, usage.DefaultCostHitPerMTok)
	if uerr != nil {
		if !errors.Is(uerr, os.ErrNotExist) {
			fmt.Fprintf(stderr, "dashboard: usage: %v (showing zeros)\n", uerr)
		}
		s = &usage.Summary{}
	}
	data.Savings = dashboardSavings{
		SavingsUSD:     s.SavingsUSD,
		SavingsRate:    s.SavingsRate,
		CostPaidUSD:    s.CostPaidUSD,
		CostNoCacheUSD: s.CostNoCacheUSD,
	}
	data.HitRate.Events = s.Events
	data.HitRate.L3Hits = s.L3Reuses
	data.HitRate.L2Hits = countL2Hits(*usagePath)
	data.HitRate.Hits = data.HitRate.L3Hits + data.HitRate.L2Hits
	if data.HitRate.Events > 0 {
		data.HitRate.Rate = float64(data.HitRate.Hits) / float64(data.HitRate.Events)
	}

	// 🗂 + 📦 from the slice library. Read-only: the dashboard must not
	// create the store, so a missing path is simply "no data yet".
	if _, err := os.Stat(db); err == nil {
		store, serr := deps.openStore(db)
		if serr != nil {
			fmt.Fprintf(stderr, "dashboard: open store: %v (showing zeros)\n", serr)
		} else {
			defer closeStore(store)
			h, g, m := libraryZones(store, deps.newIndex())
			data.Zones = dashboardZones{Hit: h, Grey: g, Miss: m, Total: h + g + m}
			n, xs := libraryTotals(store)
			data.Library = dashboardLibrary{Slices: n, CrossSessions: xs,
				Capacity: cfgInt(deps.resolved, "store.max_slices", 5000)}
		}
	}

	if *jsonOut {
		if err := writeEnvelope(stdout, "dashboard", data); err != nil {
			fmt.Fprintf(stderr, "dashboard: %v\n", err)
			return 1
		}
		return 0
	}

	color := !*noColor && isCharDevice(stdout)
	renderDashboard(stdout, data, color, *width)
	return 0
}

// dashboardPayload is the structured data behind the dashboard; it is also
// the JSON envelope payload (§4.2) when --json is set.
type dashboardPayload struct {
	Savings dashboardSavings `json:"savings"`
	HitRate dashboardHitRate `json:"hit_rate"`
	Zones   dashboardZones   `json:"zones"`
	Library dashboardLibrary `json:"library"`
}

type dashboardSavings struct {
	SavingsUSD     float64 `json:"savings_usd"`
	SavingsRate    float64 `json:"savings_rate"`
	CostPaidUSD    float64 `json:"cost_paid_usd"`
	CostNoCacheUSD float64 `json:"cost_no_cache_usd"`
}

type dashboardHitRate struct {
	Events int     `json:"events"`
	L3Hits int     `json:"l3_hits"`
	L2Hits int     `json:"l2_hits"`
	Hits   int     `json:"hits"`
	Rate   float64 `json:"rate"` // hits / events, 0..1
}

type dashboardZones struct {
	Hit   int `json:"hit"`
	Grey  int `json:"grey"`
	Miss  int `json:"miss"`
	Total int `json:"total"`
}

type dashboardLibrary struct {
	Slices        int `json:"slices"`
	CrossSessions int `json:"cross_sessions"`
	// Capacity is the configured store.max_slices (0 = unlimited).
	Capacity int `json:"capacity"`
}

// countL2Hits scans the usage log and counts turns with an L2 injection
// (InjectedTokens > 0). L3 turns come from usage.Summarize (L3Reuses);
// provider prefix cache hits (L1) are not a cache-layer reuse.
func countL2Hits(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0 // missing log: no data
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e usage.Event
		if json.Unmarshal(line, &e) != nil {
			continue // tolerate bad lines, like Summarize
		}
		if e.InjectedTokens > 0 {
			n++
		}
	}
	return n
}

// libraryZones computes the hit/grey/miss distribution by replaying the slice
// library against itself: each slice's content is searched and the top
// non-self hit is classified with the same thresholds verify uses
// (classifyTop1 + zone.Default). This is a snapshot approximation, not an
// identical replay — verify trains only Prompt slices from earlier sessions,
// while the dashboard indexes every slice in the store (all types/scopes), so
// the corpus statistics (df/idf) differ slightly. A slice with no other
// similar slice is a miss.
func libraryZones(store slice.Store, idx slice.Index) (hit, grey, miss int) {
	all, err := store.ListAll()
	if err != nil {
		return 0, 0, 0
	}
	for _, sl := range all {
		_ = idx.Insert(sl)
	}
	z := zone.Default()
	for _, sl := range all {
		q := strings.TrimSpace(string(sl.Content))
		if q == "" {
			miss++ // empty slice can never be a reusable hit
			continue
		}
		hits, err := idx.Search(q, 3, sl.Scope) // k=3: need up to 2 non-self hits
		if err != nil {
			miss++
			continue
		}
		var t1, t2 slice.Hit
		got := 0
		for _, h := range hits {
			if h.Slice.ID == sl.ID {
				continue // exclude the query's own slice
			}
			if got == 0 {
				t1 = h
				got = 1
			} else {
				t2 = h
				got = 2
				break
			}
		}
		if got == 0 {
			miss++ // no other similar slice in the library
			continue
		}
		switch classifyTop1(z, t1.Score, t2.Score) {
		case zone.Hit:
			hit++
		case zone.Grey:
			grey++
		default:
			miss++
		}
	}
	return hit, grey, miss
}

// libraryTotals counts the slices and the distinct source sessions they came
// from (SourceSession dedup — the cross-session reuse signal).
func libraryTotals(store slice.Store) (n, sessions int) {
	all, err := store.ListAll()
	if err != nil {
		return 0, 0
	}
	seen := make(map[string]struct{}, len(all))
	for _, sl := range all {
		n++
		if sl.Meta.SourceSession != "" {
			seen[sl.Meta.SourceSession] = struct{}{}
		}
	}
	return n, len(seen)
}

// renderDashboard prints the one-screen snapshot. Bars are Unicode block
// characters (█ full, ░ light shade); colors are stdlib ANSI escapes and are
// only emitted when color is enabled.
func renderDashboard(w io.Writer, d dashboardPayload, color bool, width int) {
	title := paint(color, ansiBold, "  semantix dashboard — reuse snapshot")
	fmt.Fprintln(w, title)
	fmt.Fprintln(w, "  ------------------------------------------------")
	fmt.Fprintln(w)

	// 💰 Cost savings.
	fmt.Fprintf(w, "  %s\n", paint(color, ansiBold, "💰 Cost savings"))
	fmt.Fprintf(w, "     paid        $ %.4f\n", d.Savings.CostPaidUSD)
	fmt.Fprintf(w, "     baseline    $ %.4f\n", d.Savings.CostNoCacheUSD)
	fmt.Fprintf(w, "     saved       $ %.4f  (%.2f%%)\n", d.Savings.SavingsUSD, 100*d.Savings.SavingsRate)
	fmt.Fprintf(w, "     %s\n", paint(color, ansiCyan, bar(d.Savings.SavingsRate, width)))
	fmt.Fprintln(w)

	// 🎯 Cache hit rate (L3/L2 hits / total turns).
	fmt.Fprintf(w, "  %s\n", paint(color, ansiBold, "🎯 Cache hit rate (L3/L2)"))
	rate := 0.0
	if d.HitRate.Events > 0 {
		rate = float64(d.HitRate.Hits) / float64(d.HitRate.Events)
	}
	fmt.Fprintf(w, "     %d / %d turns  (%.2f%%)\n", d.HitRate.Hits, d.HitRate.Events, 100*rate)
	fmt.Fprintf(w, "     L3 %d · L2 %d\n", d.HitRate.L3Hits, d.HitRate.L2Hits)
	fmt.Fprintf(w, "     %s\n", paint(color, ansiCyan, bar(rate, width)))
	fmt.Fprintln(w)

	// 🗂 Zone distribution (library replay, verify thresholds).
	fmt.Fprintf(w, "  %s\n", paint(color, ansiBold, "🗂 Zone distribution (library replay)"))
	if d.Zones.Total > 0 {
		fmt.Fprintf(w, "     hit  %s %d   grey %s %d   miss %s %d\n",
			paint(color, ansiGreen, barLen(d.Zones.Hit, width)),
			d.Zones.Hit,
			paint(color, ansiYellow, barLen(d.Zones.Grey, width)),
			d.Zones.Grey,
			paint(color, ansiRed, barLen(d.Zones.Miss, width)),
			d.Zones.Miss)
	} else {
		fmt.Fprintln(w, "     no slices in library — no zone data")
	}
	fmt.Fprintln(w)

	// 📦 Slice library (+ capacity water level when a cap is configured).
	fmt.Fprintf(w, "  %s\n", paint(color, ansiBold, "📦 Slice library"))
	if d.Library.Capacity > 0 {
		level := float64(d.Library.Slices) / float64(d.Library.Capacity)
		fmt.Fprintf(w, "     %d / %d slices (%.0f%%) · %d cross-session sessions\n",
			d.Library.Slices, d.Library.Capacity, 100*level, d.Library.CrossSessions)
		fmt.Fprintf(w, "     %s\n", paint(color, ansiCyan, bar(level, width)))
	} else {
		fmt.Fprintf(w, "     %d slices · %d cross-session sessions\n", d.Library.Slices, d.Library.CrossSessions)
	}
	fmt.Fprintln(w)
}

// bar renders a fill bar: fill (0..1) of width as full blocks (█), the rest
// as light-shade blocks (░). NaN and out-of-range values clamp.
func bar(fill float64, width int) string {
	if width <= 0 {
		return ""
	}
	if math.IsNaN(fill) || fill <= 0 {
		return strings.Repeat("░", width)
	}
	if fill >= 1 {
		return strings.Repeat("█", width)
	}
	n := int(math.Round(fill * float64(width)))
	if n < 0 {
		n = 0
	}
	if n > width {
		n = width
	}
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}

// barLen renders a count-proportional bar: one full block per unit, capped at
// width. Zero yields an empty bar (the numeric label still shows the count).
func barLen(n, width int) string {
	if n <= 0 || width <= 0 {
		return ""
	}
	if n > width {
		n = width
	}
	return strings.Repeat("█", n)
}

// ansi color codes (stdlib escapes, emitted only when color is enabled).
type ansiCode string

const (
	ansiReset  ansiCode = "\x1b[0m"
	ansiBold   ansiCode = "\x1b[1m"
	ansiRed    ansiCode = "\x1b[31m"
	ansiGreen  ansiCode = "\x1b[32m"
	ansiYellow ansiCode = "\x1b[33m"
	ansiCyan   ansiCode = "\x1b[36m"
)

// paint wraps s in the ANSI code when color is on, returning s unchanged
// otherwise — piped output and test buffers never carry escape bytes.
func paint(color bool, c ansiCode, s string) string {
	if !color {
		return s
	}
	return string(c) + s + string(ansiReset)
}

// isCharDevice reports whether w is a terminal-like character device (stdout
// on a real TTY). Pipes, files, and test buffers are not, so ANSI escapes are
// only emitted where they render.
func isCharDevice(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}
