package slice

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Context consolidation (W4 of the efficiency research plan): the extractor
// emits one Context slice per session, so a repo worked on across many
// sessions accumulates near-duplicate Context slices — same frequent paths
// and command heads with tiny variations. They inflate the library, split
// retrieval weight across duplicates, and waste injection budget.
//
// Consolidation merges near-duplicate Context slices into one union slice
// (token-set Jaccard over the parsed path/command entries decides). It runs
// ONLY at library-maintenance time — never during injection — because a
// merge changes slice content and therefore injection bytes; doing it
// mid-flight would break the byte-stability the provider prefix cache keys
// on. New slices get fresh content-hash IDs; members are removed.

// ConsolidateOptions configures one consolidation pass. Zero values are
// valid: Threshold falls back to 0.6.
type ConsolidateOptions struct {
	// Threshold is the token-set Jaccard above which two Context slices are
	// considered near-duplicates. 0 -> 0.6.
	Threshold float64
	// DryRun reports merges without mutating the store.
	DryRun bool
}

// ConsolidateResult summarizes one pass.
type ConsolidateResult struct {
	Checked int      // Context slices inspected
	Groups  int      // near-duplicate groups found (>= 2 members)
	Merged  int      // union slices written (0 in dry-run)
	Created []string // merged slice ids
	Removed []string // member ids folded into a merge
}

// ConsolidateContext merges near-duplicate Context slices. Deterministic:
// clusters form in ID order against each cluster's first member, so the same
// store always produces the same merge plan.
func ConsolidateContext(store Store, opts ConsolidateOptions) (ConsolidateResult, error) {
	var res ConsolidateResult
	if opts.Threshold <= 0 {
		opts.Threshold = 0.6
	}
	all, err := store.ListAll()
	if err != nil {
		return res, err
	}
	var ctx []*Slice
	for _, s := range all {
		if s.Type == Context {
			ctx = append(ctx, s)
		}
	}
	res.Checked = len(ctx)
	if len(ctx) < 2 {
		return res, nil
	}
	sort.Slice(ctx, func(i, j int) bool { return ctx[i].ID < ctx[j].ID })

	// Greedy clustering: each slice joins the first cluster whose
	// representative it matches, else becomes a new representative.
	type cluster struct {
		rep     *Slice
		members []*Slice
	}
	var clusters []*cluster
	tokens := map[string]map[string]bool{}
	for _, s := range ctx {
		tokens[s.ID] = contextTokens(string(s.Content))
	}
	for _, s := range ctx {
		joined := false
		for _, c := range clusters {
			if contextMetadataCompatible(s, c.rep) && jaccard(tokens[s.ID], tokens[c.rep.ID]) >= opts.Threshold {
				c.members = append(c.members, s)
				joined = true
				break
			}
		}
		if !joined {
			clusters = append(clusters, &cluster{rep: s, members: []*Slice{s}})
		}
	}

	for _, c := range clusters {
		if len(c.members) < 2 {
			continue
		}
		res.Groups++
		merged := mergeContextSlices(c.members)
		if merged == nil {
			continue
		}
		// Content IDs do not encode provenance. Do not replace an unrelated
		// existing card merely because the newly merged text has the same ID.
		// fileStore.Get signals "missing" as (nil, nil), but tolerate a
		// wrapped errNotFound from other Store implementations instead of
		// aborting the whole consolidation pass.
		existing, err := store.Get(merged.ID)
		if err != nil && !errors.Is(err, errNotFound) {
			return res, err
		}
		if existing != nil {
			member := false
			for _, m := range c.members {
				member = member || m.ID == existing.ID
			}
			if !member || !contextMetadataCompatible(merged, existing) {
				continue
			}
		}
		res.Created = append(res.Created, merged.ID)
		for _, m := range c.members {
			res.Removed = append(res.Removed, m.ID)
		}
		if opts.DryRun {
			continue
		}
		if err := store.Put(merged); err != nil {
			return res, err
		}
		res.Merged++
		for _, m := range c.members {
			if m.ID == merged.ID {
				continue // merge reproduces a member byte-for-byte
			}
			if err := store.Delete(m.ID); err != nil {
				return res, err
			}
		}
	}
	return res, nil
}

// Merge only evidence with the same scope, trust and freshness semantics.
// Source identities and compression byte counts are the fields a merge changes.
func contextMetadataCompatible(a, b *Slice) bool {
	if a.Scope != b.Scope {
		return false
	}
	am, bm := a.Meta, b.Meta
	am.SourceSession, bm.SourceSession = "", ""
	am.SourceSessions, bm.SourceSessions = nil, nil
	am.OriginalBytes, bm.OriginalBytes = 0, 0
	am.StoredBytes, bm.StoredBytes = 0, 0
	return reflect.DeepEqual(am, bm)
}

// RetainExtractionHistory preserves observed sources and usage feedback when
// extraction re-observes exactly the same evidence. It deliberately does not
// change Store.Put's replacement contract or carry history across trust,
// revision, dependency, scope, type or verification changes.
func RetainExtractionHistory(item, existing *Slice) {
	if existing == nil || item.ID != existing.ID || item.Type != existing.Type ||
		!bytes.Equal(item.Content, existing.Content) || !contextMetadataCompatible(item, existing) {
		return
	}
	item.Meta.SourceSessions = contextSourceSessions([]*Slice{item, existing})
	item.Stats, item.Weight = existing.Stats, existing.Weight
}

func contextSourceSessions(members []*Slice) []string {
	sources := map[string]bool{}
	for _, member := range members {
		for _, source := range append([]string{member.Meta.SourceSession}, member.Meta.SourceSessions...) {
			if source != "" {
				sources[source] = true
			}
		}
	}
	var result []string
	for source := range sources {
		result = append(result, source)
	}
	sort.Strings(result)
	return result
}

// contextTokens lowercased word tokens of a Context slice, deduped. Counts
// and section headers are deliberately excluded: the set of path/command
// values is what similarity means here.
func contextTokens(content string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, ":") || strings.HasPrefix(line, "Project context") {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		if i := strings.LastIndex(line, " ("); i > 0 && strings.HasSuffix(line, ")") {
			line = line[:i]
		}
		for _, f := range strings.Fields(strings.ToLower(line)) {
			out[f] = true
		}
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if b[t] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// contextSection is one parsed "Frequent paths:"-style section.
type contextSection struct {
	title   string
	entries map[string]int
}

// parseContextSections parses the extractor's Context slice format back into
// sections. Unparsable lines are dropped (merge is best-effort; the union of
// the parsable majority still preserves the dominant signal).
func parseContextSections(content string) []contextSection {
	var out []contextSection
	var cur *contextSection
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Project context") {
			continue
		}
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "- ") {
			out = append(out, contextSection{title: line, entries: map[string]int{}})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil || !strings.HasPrefix(line, "- ") {
			continue
		}
		entry := strings.TrimPrefix(line, "- ")
		count := 1
		if i := strings.LastIndex(entry, " ("); i > 0 && strings.HasSuffix(entry, ")") {
			var n int
			if _, err := fmt.Sscanf(entry[i+2:len(entry)-1], "%d", &n); err == nil && n > 0 {
				count = n
				entry = entry[:i]
			}
		}
		cur.entries[entry] += count
	}
	return out
}

// mergeContextSlices builds the union slice: sections in first-seen order,
// entries summed, top 8 per section by count (ties by value, matching the
// extractor's deterministic ordering). Scope: Project unless every member is
// User (which cannot happen for T/R but C is allowed there — the union
// inherits the members' common scope; mixed scopes resolve to Project, the
// lower-visibility bucket, fail-safe).
func mergeContextSlices(members []*Slice) *Slice {
	// Sections held as stable pointers (appending to an order slice would
	// invalidate pointers into its backing array).
	sections := map[string]*contextSection{}
	var order []string
	maxCreated := int64(0)
	var stats SliceStats
	var scope Scope = Project
	allUser := true
	for _, m := range members {
		mergeStats(&stats, m.Stats)
		if m.CreatedAt > maxCreated {
			maxCreated = m.CreatedAt
		}
		if m.Scope != User {
			allUser = false
		}
		for _, sec := range parseContextSections(string(m.Content)) {
			existing, ok := sections[sec.title]
			if !ok {
				cp := contextSection{title: sec.title, entries: map[string]int{}}
				for k, v := range sec.entries {
					cp.entries[k] = v
				}
				sections[sec.title] = &cp
				existing = &cp
				order = append(order, sec.title)
				continue // first member's counts land via the copy
			}
			for k, v := range sec.entries {
				existing.entries[k] += v
			}
		}
	}
	if allUser {
		scope = User
	}
	if len(order) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("Project context observed from repeated tool calls:\n")
	for _, title := range order {
		sec := sections[title]
		type entry struct {
			value string
			count int
		}
		var entries []entry
		for v, c := range sec.entries {
			entries = append(entries, entry{v, c})
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].count != entries[j].count {
				return entries[i].count > entries[j].count
			}
			return entries[i].value < entries[j].value
		})
		if len(entries) > 8 {
			entries = entries[:8]
		}
		b.WriteString(title)
		b.WriteByte('\n')
		for _, e := range entries {
			fmt.Fprintf(&b, "- %s (%d)\n", e.value, e.count)
		}
	}
	first := members[0]
	meta := first.Meta
	meta.SourceSessions = contextSourceSessions(members)
	// Compression measurements of the first member do not describe this union.
	meta.OriginalBytes, meta.StoredBytes = 0, 0
	return &Slice{
		ID:        sliceID([]byte(strings.TrimSpace(b.String())), Context, scope),
		Type:      Context,
		Scope:     scope,
		Content:   []byte(strings.TrimSpace(b.String())),
		Weight:    first.Weight,
		Stats:     stats,
		Meta:      meta,
		CreatedAt: maxCreated,
	}
}
