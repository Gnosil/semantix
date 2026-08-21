package cache

import (
	"context"
	"testing"

	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

func TestFreshnessClassifyBoundaries(t *testing.T) {
	const now = int64(1_000)
	policy := Freshness{NowUnix: now, TTLSeconds: 100}
	tests := []struct {
		name      string
		createdAt int64
		want      zone.Zone
	}{
		{"new", now, zone.Hit},
		{"half ttl", now - 50, zone.Hit},
		{"second half", now - 51, zone.Grey},
		{"ttl boundary", now - 100, zone.Grey},
		{"expired", now - 101, zone.Miss},
		{"missing timestamp", 0, zone.Miss},
		{"future timestamp", now + 1, zone.Miss},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := policy.classify(tt.createdAt); got != tt.want {
				t.Fatalf("classify(%d) = %s, want %s", tt.createdAt, got, tt.want)
			}
		})
	}
	if got := (Freshness{}).classify(0); got != zone.Hit {
		t.Fatalf("disabled policy = %s, want neutral hit", got)
	}
}

func TestDecideL3AgedHitRequiresJudge(t *testing.T) {
	const now = int64(1_000)
	h := hitOf("aged", slice.Result, 1)
	h.Slice.CreatedAt = now - 60
	idx := &fixedIndex{hits: []slice.Hit{h}}
	q := Query{UserInput: "same task", Scope: slice.Project,
		Freshness: Freshness{NowUnix: now, TTLSeconds: 100}}

	withoutJudge := &L3Decider{Index: idx, Root: t.TempDir()}
	if got, err := withoutJudge.DecideL3(context.Background(), q); err != nil || got != nil {
		t.Fatalf("aged hit without judge = %+v, %v; want nil, nil", got, err)
	}

	mj := &mockJudge{confirm: true}
	withJudge := &L3Decider{Index: idx, Root: t.TempDir(), Judge: mj}
	got, err := withJudge.DecideL3(context.Background(), q)
	if err != nil || got == nil || got.SliceID != "aged" {
		t.Fatalf("judge-approved aged hit = %+v, %v; want aged", got, err)
	}
	if mj.got == nil || mj.got.Zone != zone.Grey {
		t.Fatalf("freshness downgrade did not enter grey judge path: %+v", mj.got)
	}
}

func TestDecideL3ExpiredCandidateSkipsToFreshCandidate(t *testing.T) {
	const now = int64(1_000)
	expired := hitOf("expired", slice.Result, 1)
	expired.Slice.CreatedAt = now - 101
	fresh := hitOf("fresh", slice.Result, 0.95)
	fresh.Slice.CreatedAt = now - 10
	idx := &fixedIndex{hits: []slice.Hit{expired, fresh}}
	mj := &mockJudge{confirm: true}
	d := &L3Decider{Index: idx, Root: t.TempDir(), Judge: mj}
	q := Query{UserInput: "same task", Scope: slice.Project,
		Freshness: Freshness{NowUnix: now, TTLSeconds: 100}}

	got, err := d.DecideL3(context.Background(), q)
	if err != nil || got == nil || got.SliceID != "fresh" {
		t.Fatalf("DecideL3 = %+v, %v; want fresh candidate", got, err)
	}
	if mj.got != nil {
		t.Fatalf("expired candidate must not spend a judge call: %+v", mj.got)
	}
}
