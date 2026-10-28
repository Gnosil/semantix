package main

import (
	"semantix/harness/stats"
	"sort"
	"strings"
)

// Ledger entries retain each request's model even after the current model is
// changed. Legacy unassigned usage is kept separate instead of attributed to
// today's model. Shares use the same input+output basis as the ledger.
func modelUsageForTelemetry(usage sessionUsageStats) []stats.ModelUsage {
	byModel := map[string]stats.ModelUsage{}
	if usage.CostLedger == nil {
		return nil
	}
	var total int64
	for _, entry := range usage.CostLedger.Entries {
		name := entry.ModelRef
		if entry.Quote.LegacyEstimate {
			name = ""
		}
		row := byModel[name]
		row.Model = name
		if parts := strings.SplitN(name, "/", 2); len(parts) == 2 {
			row.Provider = parts[0]
		}
		row.Tokens += int64(max(0, entry.TotalTokens))
		row.Requests += max(0, entry.RequestCount)
		byModel[name] = row
		total += int64(max(0, entry.TotalTokens))
	}
	rows := make([]stats.ModelUsage, 0, len(byModel))
	for _, row := range byModel {
		if total > 0 {
			row.Percent = float64(row.Tokens) / float64(total) * 100
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Tokens == rows[j].Tokens {
			return rows[i].Model < rows[j].Model
		}
		return rows[i].Tokens > rows[j].Tokens
	})
	return rows
}
