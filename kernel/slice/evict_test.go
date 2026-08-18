package slice

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func evictFixtureNow() int64 { return int64(1_700_000_000) }

// Cap eviction is deterministic: same library + same Now → identical evicted
// set, identical order, twice in a row. Ties break Weight → CreatedAt → ID.
func TestGCCapDeterministic(t *testing.T) {
	now := evictFixtureNow()
	build := func() (Store, string) {
		path := filepath.Join(t.TempDir(), "s.jsonl")
		st := reopen(t, path)
		// Same age + same stats → same rescored weight → ID is the tiebreak.
		for _, id := range []string{"tie-c", "tie-a", "tie-b"} {
			mustPut(t, st, &Slice{ID: id, Type: Result, Scope: Project, Content: []byte(id),
				CreatedAt: now - 100*day})
		}
		// A clearly-more-valuable slice that must survive.
		mustPut(t, st, &Slice{ID: "hot", Type: Result, Scope: Project, Content: []byte("hot"),
			CreatedAt: now - 100*day, Stats: SliceStats{Hits: 40, LastUsed: now - day}})
		return st, path
	}
	runGCOnce := func() (GCResult, []string) {
		st, path := build()
		res, err := GC(st, GCOptions{MaxSlices: 2, Rescore: true, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		ids := idsOf(t, reopen(t, path))
		return res, ids
	}
	r1, ids1 := runGCOnce()
	r2, ids2 := runGCOnce()
	if !reflect.DeepEqual(r1.OverCap, r2.OverCap) || !reflect.DeepEqual(ids1, ids2) {
		t.Fatalf("nondeterministic eviction: %v/%v vs %v/%v", r1.OverCap, ids1, r2.OverCap, ids2)
	}
	if want := []string{"tie-a", "tie-b"}; !reflect.DeepEqual(r1.OverCap, want) {
		t.Fatalf("OverCap = %v, want %v (weight tie → ID asc evicts first)", r1.OverCap, want)
	}
	sort.Strings(ids1)
	if want := []string{"hot", "tie-c"}; !reflect.DeepEqual(ids1, want) {
		t.Fatalf("survivors = %v, want %v", ids1, want)
	}
}

// Grace-window slices sort last but the cap still holds even when everything
// is inside grace — the cap is never exceeded.
func TestGCCapGraceOrderingAndForce(t *testing.T) {
	now := evictFixtureNow()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	mustPut(t, st, &Slice{ID: "old-weak", Type: Result, Scope: Project, Content: []byte("x"),
		CreatedAt: now - 200*day})
	mustPut(t, st, &Slice{ID: "fresh", Type: Result, Scope: Project, Content: []byte("y"),
		CreatedAt: now - day}) // inside 7d grace, weaker score would evict it without grace? no — fresh scores higher anyway; force explicit check below
	res, err := GC(st, GCOptions{MaxSlices: 1, Rescore: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.OverCap, []string{"old-weak"}) {
		t.Fatalf("grace must push fresh slices to the back of the eviction order: %v", res.OverCap)
	}

	// All-grace library: cap still enforced.
	path2 := filepath.Join(t.TempDir(), "s.jsonl")
	st2 := reopen(t, path2)
	for i := 0; i < 3; i++ {
		mustPut(t, st2, &Slice{ID: fmt.Sprintf("g%d", i), Type: Result, Scope: Project,
			Content: []byte("z"), CreatedAt: now - int64(i)*3600})
	}
	res2, err := GC(st2, GCOptions{MaxSlices: 1, Rescore: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Removed != 2 {
		t.Fatalf("cap must hold on an all-grace library: removed %d, want 2", res2.Removed)
	}
	if got := idsOf(t, reopen(t, path2)); len(got) != 1 {
		t.Fatalf("post-gc size = %d, want 1", len(got))
	}
}

// Legacy slices (no CreatedAt, Weight 0) keep their threshold exemptions but
// DO compete under the cap — and lose ties as "oldest".
func TestGCCapLegacyParticipates(t *testing.T) {
	now := evictFixtureNow()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	// Handwritten v1 line: no created_at, no weight — the legacy shape.
	base := `{"id":"legacy","type":2,"scope":1,"content":"bGVnYWN5"}` + "\n"
	if err := os.WriteFile(path, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	st := reopen(t, path)
	mustPut(t, st, &Slice{ID: "used", Type: Result, Scope: Project, Content: []byte("u"),
		CreatedAt: now - 30*day, Stats: SliceStats{Hits: 10, LastUsed: now - day}})

	// Threshold-only dry-run: legacy is exempt from both criteria even after
	// cap features exist (no cap configured; dry so the aggressive retention
	// doesn't consume the fixture).
	res, err := GC(st, GCOptions{RetentionDays: 7, MinWeight: 0.9, Now: now, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if containsID(res.Expired, "legacy") || containsID(res.LowScore, "legacy") {
		t.Fatalf("legacy exemptions lost: %+v", res)
	}

	// Cap run: legacy competes (neutral prior ≈ 0.07 < used's score) and is
	// evicted first.
	res2, err := GC(st, GCOptions{MaxSlices: 1, Rescore: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res2.OverCap, []string{"legacy"}) {
		t.Fatalf("legacy must compete under the cap: %v", res2.OverCap)
	}
}

// Evicted slices land in the archive in Export format and import back
// losslessly — including raw embedding bytes the read path never decodes.
func TestGCArchiveRoundTrip(t *testing.T) {
	now := evictFixtureNow()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	base := `{"ID":"vec-old","Type":2,"Scope":1,"Content":"dg==","Embedding":[0,0.5,-0.25],"created_at":` +
		fmt.Sprint(now-300*day) + `}` + "\n"
	if err := os.WriteFile(path, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	st := reopen(t, path)
	mustPut(t, st, &Slice{ID: "keep", Type: Result, Scope: Project, Content: []byte("k"),
		CreatedAt: now, Stats: SliceStats{Hits: 9, LastUsed: now}})

	archive := path + ".archive.jsonl"
	res, err := GC(st, GCOptions{MaxSlices: 1, Rescore: true, Now: now, ArchivePath: archive})
	if err != nil {
		t.Fatal(err)
	}
	if res.Archived != 1 || !reflect.DeepEqual(res.OverCap, []string{"vec-old"}) {
		t.Fatalf("archive result wrong: %+v", res)
	}
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"Embedding":[0,0.5,-0.25]`)) {
		t.Fatalf("archive lost raw vector bytes:\n%s", raw)
	}
	if perm := mustStat(t, archive).Mode().Perm(); perm != 0o600 {
		t.Fatalf("archive perm = %o, want 600", perm)
	}

	// Restore: import the archive back, slice returns whole.
	dst := reopen(t, filepath.Join(t.TempDir(), "dst.jsonl"))
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	imported, skipped, err := Import(dst, f)
	if err != nil || imported != 1 || skipped != 0 {
		t.Fatalf("import from archive = %d/%d/%v", imported, skipped, err)
	}
	got, _ := dst.Get("vec-old")
	if got == nil || string(got.Content) != "v" {
		t.Fatalf("restored slice wrong: %+v", got)
	}
}

// Dry-run must persist nothing: no archive file, no rescored weights on
// disk, no evictions — while still reporting the full plan.
func TestGCDryRunPersistsNothing(t *testing.T) {
	now := evictFixtureNow()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	for i := 0; i < 3; i++ {
		mustPut(t, st, &Slice{ID: fmt.Sprintf("s%d", i), Type: Result, Scope: Project,
			Content: []byte("x"), CreatedAt: now - 100*day})
	}
	archive := path + ".archive.jsonl"
	res, err := GC(st, GCOptions{MaxSlices: 1, Rescore: true, Now: now, DryRun: true, ArchivePath: archive})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 || len(res.OverCap) != 2 || res.RescoredWeights != 3 {
		t.Fatalf("dry-run plan wrong: %+v", res)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote an archive")
	}
	fresh := reopen(t, path)
	all, _ := fresh.ListAll()
	if len(all) != 3 {
		t.Fatalf("dry-run evicted: %d slices left", len(all))
	}
	for _, sl := range all {
		if sl.Weight != 1.0 { // extractor-style initial weight from Put
			t.Fatalf("dry-run persisted a rescored weight: %s = %v", sl.ID, sl.Weight)
		}
	}
}

// After a scoring GC, weights are persisted and --min-weight has real teeth
// (the criterion was dead while Weight was uniformly 1.0).
func TestGCRescorePersistsAndRevivesMinWeight(t *testing.T) {
	now := evictFixtureNow()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	mustPut(t, st, &Slice{ID: "stale", Type: Result, Scope: Project, Content: []byte("s"),
		CreatedAt: now - 400*day})
	mustPut(t, st, &Slice{ID: "hot", Type: Result, Scope: Project, Content: []byte("h"),
		CreatedAt: now - 10*day, Stats: SliceStats{Hits: 30, LastUsed: now}})
	if _, err := GC(st, GCOptions{Rescore: true, Now: now}); err != nil {
		t.Fatal(err)
	}
	fresh := reopen(t, path)
	stale, _ := fresh.Get("stale")
	hot, _ := fresh.Get("hot")
	if stale.Weight >= hot.Weight || stale.Weight == 1.0 {
		t.Fatalf("weights not persisted: stale=%v hot=%v", stale.Weight, hot.Weight)
	}
	res, err := GC(fresh, GCOptions{MinWeight: 0.05, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !containsID(res.LowScore, "stale") || containsID(res.LowScore, "hot") {
		t.Fatalf("--min-weight still dead after rescore: %+v", res)
	}
}

func mustPut(t *testing.T, st Store, sl *Slice) {
	t.Helper()
	sl.Weight = 1.0 // extractor initial weight
	if err := st.Put(sl); err != nil {
		t.Fatal(err)
	}
}

func idsOf(t *testing.T, st Store) []string {
	t.Helper()
	all, err := st.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(all))
	for _, sl := range all {
		ids = append(ids, sl.ID)
	}
	return ids
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

var _ = strings.TrimSpace // keep strings import if assertions above change
