package main

import (
	"encoding/json"
	"fmt"
	"os"

	"semantix/kernel/slice"
)

// loadScoreParams reads the offline fitter's ScoreParams snapshot
// (docs/specs/local-retrieval-model.md §3 C). Keys are snake_case, matching
// the [score] config block, so a snapshot can be pasted into config
// verbatim. Values ≤0 fall back to the kernel defaults via withDefaults()
// inside ComputeWeight, same as the config path.
func loadScoreParams(path string) (slice.ScoreParams, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return slice.ScoreParams{}, err
	}
	var f struct {
		HalfLifeDays float64 `json:"half_life_days"`
		GraceDays    float64 `json:"grace_days"`
		FreqPseudo   float64 `json:"freq_pseudo"`
		RejectCost   float64 `json:"reject_cost"`
		FeedbackGain float64 `json:"feedback_gain"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return slice.ScoreParams{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return slice.ScoreParams{
		HalfLifeDays: f.HalfLifeDays,
		GraceDays:    f.GraceDays,
		FreqPseudo:   f.FreqPseudo,
		RejectCost:   f.RejectCost,
		FeedbackGain: f.FeedbackGain,
	}, nil
}
