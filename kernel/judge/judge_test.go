package judge

import (
	"context"
	"errors"
	"testing"

	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

func cand(z zone.Zone) Candidate {
	return Candidate{Query: "q", SliceID: "s", Content: "a", Scope: slice.Project, Type: slice.Prompt, Zone: z}
}

func TestRuleGateZones(t *testing.T) {
	g := RuleGate{}
	if v, _ := g.Check(cand(zone.Hit)); v != Confirm {
		t.Errorf("hit -> %v, want Confirm", v)
	}
	if v, _ := g.Check(cand(zone.Miss)); v != Reject {
		t.Errorf("miss -> %v, want Reject", v)
	}
	if v, _ := g.Check(cand(zone.Grey)); v != Reject {
		t.Errorf("grey (no judge) -> %v, want Reject (conservative)", v)
	}
}

func TestRuleGateGreyNeedsJudge(t *testing.T) {
	g := RuleGate{Judge: NoopJudge{}}
	if v, _ := g.Check(cand(zone.Grey)); v != NeedJudge {
		t.Errorf("grey (with judge) -> %v, want NeedJudge", v)
	}
}

type stubJudge struct{ ok bool }

func (s stubJudge) Confirm(context.Context, Candidate) (bool, error) { return s.ok, nil }

func TestChainGreyNoJudgeRejects(t *testing.T) {
	g := RuleGate{}
	v, reason, err := g.Chain(context.Background(), cand(zone.Grey))
	if err != nil || v != Reject {
		t.Fatalf("chain = %v %q %v, want Reject (conservative, never NeedJudge)", v, reason, err)
	}
	if reason != "grey zone: no judge wired, conservative reject" {
		t.Errorf("reason = %q", reason)
	}
}

func TestChainGreyJudgeApproves(t *testing.T) {
	g := RuleGate{Judge: stubJudge{ok: true}}
	v, reason, err := g.Chain(context.Background(), cand(zone.Grey))
	if err != nil || v != Confirm || reason != "judge approved" {
		t.Fatalf("chain = %v %q %v, want Confirm", v, reason, err)
	}
}

func TestChainGreyJudgeDeclines(t *testing.T) {
	g := RuleGate{Judge: stubJudge{ok: false}}
	v, _, err := g.Chain(context.Background(), cand(zone.Grey))
	if err != nil || v != Reject {
		t.Fatalf("chain = %v %v, want Reject", v, err)
	}
}

func TestChainJudgeErrorConservative(t *testing.T) {
	g := RuleGate{Judge: errJudge{}}
	v, _, err := g.Chain(context.Background(), cand(zone.Grey))
	if err == nil || v != Reject {
		t.Fatalf("chain = %v %v, want Reject with error", v, err)
	}
}

// TestChainStatsAccuracy guards Issue #245: judge call errors must land in
// JudgeError (not RulesReject / JudgeReject), so observability can tell
// "judge unavailable" apart from "judge declined".
func TestChainStatsAccuracy(t *testing.T) {
	tests := []struct {
		name  string
		judge Judge
		want  Stats
	}{
		{"judge error -> JudgeError", errJudge{}, Stats{JudgeError: 1, NeedJudge: 1}},
		{"judge declined -> JudgeReject", stubJudge{ok: false}, Stats{JudgeReject: 1, NeedJudge: 1}},
		{"judge approved -> Confirmed+JudgeApproved", stubJudge{ok: true}, Stats{Confirmed: 1, JudgeApproved: 1, NeedJudge: 1}},
		{"grey no judge -> RulesReject", nil, Stats{RulesReject: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Stats
			g := RuleGate{Judge: tt.judge, Stats: &got}
			if _, _, err := g.Chain(context.Background(), cand(zone.Grey)); err != nil && tt.name != "judge error -> JudgeError" {
				t.Fatalf("chain: %v", err)
			}
			if got != tt.want {
				t.Errorf("stats = %+v, want %+v", got, tt.want)
			}
		})
	}
}

type errJudge struct{}

func (errJudge) Confirm(context.Context, Candidate) (bool, error) {
	return false, errors.New("model unavailable")
}

func TestChainHitSkipsJudge(t *testing.T) {
	// hit candidates must never reach the judge (cost).
	reached := false
	g := RuleGate{Judge: countingJudge{fn: func() { reached = true }}}
	if v, _, err := g.Chain(context.Background(), cand(zone.Hit)); err != nil || v != Confirm {
		t.Fatalf("chain = %v %v, want Confirm", v, err)
	}
	if reached {
		t.Fatal("hit candidate must not reach the judge")
	}
}

type countingJudge struct{ fn func() }

func (c countingJudge) Confirm(ctx context.Context, cand Candidate) (bool, error) {
	c.fn()
	return true, nil
}
