package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"semantix/kernel/zone"
)

// helper: write a session JSONL file with the given user turns.
func writeSession(t *testing.T, dir, name string, users []string) string {
	t.Helper()
	var b strings.Builder
	for _, u := range users {
		b.WriteString(`{"role":"user","content":"` + u + `"}` + "\n")
		b.WriteString(`{"role":"assistant","content":"done"}` + "\n")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	// mtime controls train/replay split; bump each file by a minute.
	mt := time.Now().Add(time.Duration(len(dir))*time.Minute + time.Minute)
	_ = os.Chtimes(path, mt, mt)
	return path
}

func TestVerifyReplayProducesEvaluationTable(t *testing.T) {
	dir := t.TempDir()
	// 3 sessions: first two are "earlier" (train), last is replay.
	writeSession(t, dir, "s1.jsonl", []string{"修复 go 测试失败", "加 CI 配置"})
	writeSession(t, dir, "s2.jsonl", []string{"修复 go 测试失败", "优化查询性能"})
	writeSession(t, dir, "s3.jsonl", []string{"修复 go 测试失败", "部署到服务器"})

	var out bytes.Buffer
	db := filepath.Join(t.TempDir(), "v.db")
	code := runVerify([]string{"--session", dir, "--db", db, "--holdout", "0.33"}, &out, productionDependencies())
	if code != 0 {
		t.Fatalf("runVerify code = %d, want 0; out:\n%s", code, out.String())
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var rows int
	for _, l := range lines {
		if strings.HasPrefix(l, "#") || strings.HasPrefix(l, "session\t") {
			continue
		}
		rows++
		cols := strings.Split(l, "\t")
		if len(cols) != 6 {
			t.Fatalf("row %q: want 6 tab columns (session/turn/score/zone/top1/query), got %d", l, len(cols))
		}
		if cols[3] != "✅hit" && cols[3] != "🟡grey" && cols[3] != "❌miss" {
			t.Fatalf("row %q: zone column %q not an iconized zone", l, cols[3])
		}
	}
	if rows != 2 {
		t.Fatalf("replay rows = %d, want 2 (one per replay-session turn)", rows)
	}
	// The replayed "修复 go 测试失败" turn must have a trained top-1 hit
	// (score > 0) — the exact same instruction exists in s1/s2.
	var foundScore bool
	for _, l := range lines {
		if strings.HasPrefix(l, "#") || strings.HasPrefix(l, "session\t") {
			continue
		}
		cols := strings.Split(l, "\t")
		if strings.Contains(cols[4], "修复 go 测试失败") {
			foundScore = cols[2] != "0.0000"
		}
	}
	if !foundScore {
		t.Fatalf("replayed '修复 go 测试失败' turn did not get a scored top-1 hit:\n%s", out.String())
	}
}

// TestVerifyIconicOutput verifies the Issue #153 / U29 human-readable output:
// iconized zone cells, the zone distribution bar, and the gate verdict line.
func TestVerifyIconicOutput(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "s1.jsonl", []string{"修复 go 测试失败"})
	writeSession(t, dir, "s2.jsonl", []string{"修复 go 测试失败"})

	var out bytes.Buffer
	db := filepath.Join(t.TempDir(), "v.db")
	code := runVerify([]string{"--session", dir, "--db", db, "--holdout", "0.5"}, &out, productionDependencies())
	if code != 0 {
		t.Fatalf("runVerify = %d, want 0; out:\n%s", code, out.String())
	}
	s := out.String()
	for _, want := range []string{
		"✅hit",               // iconized zone cell
		"# zones: hit",        // distribution bar line
		"✅ PASS relevance=",  // gate verdict line
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("output missing %q:\n%s", want, s)
		}
	}
}

func TestVerifyRequiresTwoSessions(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "only.jsonl", []string{"x"})
	var out bytes.Buffer
	code := runVerify([]string{"--session", dir}, &out, productionDependencies())
	if code != 1 {
		t.Fatalf("runVerify with 1 session = code %d, want 1", code)
	}
	if !strings.Contains(out.String(), "need at least 2 session files") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestVerifyRejectsDuplicateSessionFiles(t *testing.T) {
	for _, form := range []string{"repeated file", "directory and child", "symlink", "hard link"} {
		t.Run(form, func(t *testing.T) {
			dir := t.TempDir()
			original := writeSession(t, dir, "original.jsonl", []string{"synthetic cobalt orchard route"})
			first, second := original, original
			switch form {
			case "directory and child":
				first = dir
			case "symlink", "hard link":
				second = filepath.Join(dir, "alias.jsonl")
				link := os.Symlink
				if form == "hard link" {
					link = os.Link
				}
				if err := link(original, second); err != nil {
					t.Skipf("creating a %s is not available: %v", form, err)
				}
			}
			var out bytes.Buffer
			db := filepath.Join(t.TempDir(), "v.db")
			args := []string{"--session", first, "--session", second, "--db", db, "--holdout", "0.5"}
			if code := runVerify(args, &out, productionDependencies()); code != 1 ||
				out.String() != "verify: duplicate session file; pass each session only once\n" {
				t.Fatalf("expected duplicate error without source paths; code=%d out=%q", code, &out)
			}
			if _, err := os.Stat(db); !os.IsNotExist(err) {
				t.Fatalf("validation must precede replay database creation; stat error=%v", err)
			}
		})
	}
}

func TestVerifyKeepsDistinctSessionsWithIdenticalPrompts(t *testing.T) {
	dir := t.TempDir()
	earlier := writeSession(t, dir, "earlier.jsonl", []string{"synthetic cobalt orchard route"})
	later := writeSession(t, dir, "later.jsonl", []string{"synthetic cobalt orchard route"})
	for i, path := range []string{earlier, later} {
		mtime := time.Unix(1000+int64(i)*60, 0)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	db := filepath.Join(t.TempDir(), "v.db")
	args := []string{"--session", later, "--session", earlier, "--db", db, "--holdout", "0.5"}
	if code := runVerify(args, &out, productionDependencies()); code != 0 ||
		!strings.Contains(out.String(), "\nlater.jsonl\t1\t") || strings.Contains(out.String(), "\nearlier.jsonl\t1\t") {
		t.Fatalf("distinct files must remain eligible and split by mtime; code=%d out=%q", code, &out)
	}
}

func TestVerifyToleratesCorruptLines(t *testing.T) {
	dir := t.TempDir()
	p1 := writeSession(t, dir, "s1.jsonl", []string{"a", "b"})
	// corrupt one line in s1 (must be skipped, not fatal)
	raw, _ := os.ReadFile(p1)
	corrupt := string(raw) + "{not json}\n"
	_ = os.WriteFile(p1, []byte(corrupt), 0o600)
	writeSession(t, dir, "s2.jsonl", []string{"c"})

	var out bytes.Buffer
	code := runVerify([]string{"--session", dir}, &out, productionDependencies())
	if code != 0 {
		t.Fatalf("runVerify = code %d, want 0; out:\n%s", code, out.String())
	}
}

func TestVerifyGreyRatioAlarm(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "s1.jsonl", []string{"修复 go 测试失败", "配置 CI"})
	writeSession(t, dir, "s2.jsonl", []string{"修复 go 测试失败", "配置 CI"})

	// --abs-high 100 makes every scored top-1 land in the grey zone, so the
	// grey-ratio alarm (Issue #7 acceptance) must fire. Non-strict: exit 0
	// with a WARN line; strict: exit 3.
	cases := []struct {
		name   string
		strict bool
		want   int
	}{
		{"non-strict warn", false, 0},
		{"strict alarm", true, 3},
	}
	for _, c := range cases {
		var out bytes.Buffer
		args := []string{"--session", dir, "--db", filepath.Join(t.TempDir(), "v.db"), "--holdout", "0.5", "--abs-high", "100", "--grey-target", "10"}
		if c.strict {
			args = append(args, "--strict")
		}
		if code := runVerify(args, &out, productionDependencies()); code != c.want {
			t.Errorf("%s: code = %d, want %d; out:\n%s", c.name, code, c.want, out.String())
		}
		if !strings.Contains(out.String(), "WARN: grey_ratio") {
			t.Errorf("%s: missing WARN line:\n%s", c.name, out.String())
		}
	}
}

func TestVerifyGreyRatioAlarmSilentUnderTarget(t *testing.T) {
	dir := t.TempDir()
	// Exact-repeat replay turns produce clear hits (gap to runner-up large),
	// so grey_ratio stays 0% and no alarm fires even with --strict.
	writeSession(t, dir, "s1.jsonl", []string{"修复 go 测试失败"})
	writeSession(t, dir, "s2.jsonl", []string{"修复 go 测试失败"})

	var out bytes.Buffer
	args := []string{"--session", dir, "--db", filepath.Join(t.TempDir(), "v.db"), "--holdout", "0.5", "--strict", "--grey-target", "10"}
	if code := runVerify(args, &out, productionDependencies()); code != 0 {
		t.Fatalf("code = %d, want 0; out:\n%s", code, out.String())
	}
	if strings.Contains(out.String(), "WARN") {
		t.Fatalf("unexpected WARN under target:\n%s", out.String())
	}
}

func TestClassifyTop1(t *testing.T) {
	z := zone.Default()
	cases := []struct {
		name          string
		top1, top2    float64
		want          zone.Zone
	}{
		{"no hit", 0, 0, zone.Miss},
		{"absolute weak", 0.3, 0.1, zone.Miss},              // top1 < AbsLow
		{"nan top1", math.NaN(), 0, zone.Miss},              // failure-safe
		{"nan top2", 0.5, math.NaN(), zone.Miss},
		{"inf top1", math.Inf(1), 0, zone.Miss},
		{"inf top2", 0.5, math.Inf(1), zone.Miss},
		{"clear winner", 3.0, 0.8, zone.Hit},                // top1 >= AbsHigh, gap >= 0.55*top1
		{"near tie bm25", 3.0, 2.9, zone.Grey},              // gap 0.1 < 1.65
		{"near tie cosine", 0.75, 0.72, zone.Grey},          // gap 0.03 < 0.41
		{"weak but separated", 0.6, 0.2, zone.Grey},         // top1 < AbsHigh -> grey
		{"strong separated", 0.85, 0.3, zone.Hit},           // gap 0.55 >= 0.4675
	}
	for _, c := range cases {
		if got := classifyTop1(z, c.top1, c.top2); got != c.want {
			t.Errorf("%s: classifyTop1(%v, %v) = %v, want %v", c.name, c.top1, c.top2, got, c.want)
		}
	}
}

// --- Issue #262: verify calibration report (per-bin hit/miss distribution
// + three-region drift vs default thresholds) ---

// calibSessions writes the standard 3-session calibration fixture: the
// replay stream (s3) re-asks a trained query and one unrelated query.
func calibSessions(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeSession(t, dir, "s1.jsonl", []string{"修复 go 测试失败", "加 CI 配置"})
	writeSession(t, dir, "s2.jsonl", []string{"修复 go 测试失败", "优化查询性能"})
	writeSession(t, dir, "s3.jsonl", []string{"修复 go 测试失败", "部署到服务器"})
	return dir
}

func TestVerifyCalibrateReportStructure(t *testing.T) {
	dir := calibSessions(t)
	var out bytes.Buffer
	db := filepath.Join(t.TempDir(), "v.db")
	code := runVerify([]string{"--session", dir, "--db", db, "--holdout", "0.33", "--calibrate"}, &out, productionDependencies())
	if code != 0 {
		t.Fatalf("runVerify code = %d, want 0; out:\n%s", code, out.String())
	}
	s := out.String()
	for _, want := range []string{
		"# calibration: 分桶命中分布",
		"#   bin        n   hit grey miss  hit%   top1_avg",
		"# drift vs default thresholds (tau_high=0.80 tau_low=0.55 abs_high=0.70 abs_low=0.45)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("calibration report missing %q:\n%s", want, s)
		}
	}
	// With default thresholds the current and baseline distributions are
	// identical — the drift block must show zero deltas.
	for _, want := range []string{"#   hit       50.0%    50.0%   +0.0pt", "#   grey       0.0%     0.0%   +0.0pt"} {
		if !strings.Contains(s, want) {
			t.Fatalf("default-threshold run must show zero drift, missing %q:\n%s", want, s)
		}
	}
	// Per-type block (Issue #259 阶段 2): the replay stream's top-1
	// candidates are typed, so the by_type report must name at least one
	// real slice type and sum its counts.
	if !strings.Contains(s, "# by_type:") {
		t.Fatalf("calibration report missing per-type block:\n%s", s)
	}
	if !strings.Contains(s, "#   type          n   hit grey miss  hit%  default_hit") {
		t.Fatalf("calibration report missing by_type header:\n%s", s)
	}
	// The report is a tail block: the replay table must precede it.
	if strings.Index(s, "session\tturn\tscore") > strings.Index(s, "# calibration:") {
		t.Fatalf("calibration block must follow the replay table:\n%s", s)
	}
}

func TestVerifyCalibrateDetectsMistunedThreshold(t *testing.T) {
	// Issue #262 acceptance: "人为调坏 TauHigh 后校准报告能显示失配".
	// --abs-high 7 pushes the reuse floor above every actual top-1 score
	// (BM25 ~6.26 for the replayed query), so the current distribution
	// must drift away from the default baseline in the report.
	dir := calibSessions(t)
	var out bytes.Buffer
	db := filepath.Join(t.TempDir(), "v.db")
	code := runVerify([]string{"--session", dir, "--db", db, "--holdout", "0.33", "--calibrate", "--abs-high", "7"}, &out, productionDependencies())
	if code != 0 {
		t.Fatalf("runVerify code = %d, want 0; out:\n%s", code, out.String())
	}
	s := out.String()
	// The replayed hit becomes grey: hit share collapses, grey rises.
	for _, want := range []string{"#   grey      50.0%     0.0%  +50.0pt", "#   hit        0.0%    50.0%  -50.0pt"} {
		if !strings.Contains(s, want) {
			t.Fatalf("mistuned threshold must be visible in the drift block, missing %q:\n%s", want, s)
		}
	}
}

func TestVerifyCalibrateLabelsPrecision(t *testing.T) {
	dir := calibSessions(t)
	labels := filepath.Join(t.TempDir(), "labels.tsv")
	// Oracle: the replayed hit is NOT relevant (false hit), the miss is.
	if err := os.WriteFile(labels, []byte("s3.jsonl\t1\t0\ns3.jsonl\t2\t1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	db := filepath.Join(t.TempDir(), "v.db")
	code := runVerify([]string{"--session", dir, "--db", db, "--holdout", "0.33", "--labels", labels}, &out, productionDependencies())
	if code != 0 {
		t.Fatalf("runVerify code = %d, want 0; out:\n%s", code, out.String())
	}
	s := out.String()
	// --labels implies the calibration report and adds fp/precision columns.
	if !strings.Contains(s, "fp  precision") {
		t.Fatalf("labeled report must add fp/precision columns:\n%s", s)
	}
	// The replayed hit (oracle 0) is a false positive: fp=1, precision 0%.
	found := false
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "#   [0.00,0.10)") && strings.Contains(l, "    1    0.0%") {
			found = true
		}
	}
	if !found {
		t.Fatalf("labeled fp/precision missing (want fp=1 precision=0.0%%):\n%s", s)
	}
}

func TestVerifyCalibrateJSONEnvelope(t *testing.T) {
	dir := calibSessions(t)
	var out bytes.Buffer
	db := filepath.Join(t.TempDir(), "v.db")
	code := runVerify([]string{"--session", dir, "--db", db, "--holdout", "0.33", "--calibrate", "--json"}, &out, productionDependencies())
	if code != 0 {
		t.Fatalf("runVerify code = %d, want 0; out:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"calibration"`) {
		t.Fatalf("--json --calibrate must carry the calibration object:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"bins"`) || !strings.Contains(out.String(), `"current"`) ||
		!strings.Contains(out.String(), `"default"`) || !strings.Contains(out.String(), `"labeled"`) {
		t.Fatalf("calibration object must expose bins/current/default/labeled:\n%s", out.String())
	}
}

func TestReadVerifyLabelsMalformed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.tsv")
	if err := os.WriteFile(p, []byte("s1\tx\t1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readVerifyLabels(p); err == nil {
		t.Fatal("malformed turn must error")
	}
	if err := os.WriteFile(p, []byte("s1\t1\tyes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readVerifyLabels(p); err == nil {
		t.Fatal("malformed relevance must error")
	}
}
