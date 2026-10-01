package main

import (
	"semantix/harness/billing"
	"testing"
	"time"
)

func TestModelUsageKeepsModelsAndSourcesAcrossSwitch(t *testing.T) {
	l := billing.NewLedger()
	for _, call := range []struct {
		model, source string
		tokens        int
	}{{"provider/model-a", "executor", 800}, {"provider/model-b", "executor", 1200}, {"provider/model-a", "planner", 200}} {
		q := billing.BuildQuote(billing.QuoteInput{Usage: billing.UsageTokens{PromptTokens: call.tokens}, Rates: billing.RateCard{Input: 3, Currency: "USD"}, ModelRef: call.model, UsageSource: call.source})
		l.Add(q, billing.UsageTokens{PromptTokens: call.tokens}, time.Now())
	}
	rows := modelUsageForTelemetry(sessionUsageStats{CostLedger: l})
	if len(rows) != 2 || rows[0].Model != "provider/model-b" || rows[0].Tokens != 1200 || rows[1].Tokens != 1000 || rows[1].Requests != 2 {
		t.Fatalf("models=%+v", rows)
	}
	if rows[0].Percent+rows[1].Percent < 99.99 {
		t.Fatal("shares do not cover model usage")
	}
}

func TestModelUsageDoesNotAttributeLegacyToCurrentModel(t *testing.T) {
	l := billing.NewLedger()
	l.Add(billing.CostQuote{Original: billing.MoneyOf(billing.Zero, "USD"), ModelRef: "current/model", LegacyEstimate: true}, billing.UsageTokens{PromptTokens: 300}, time.Now())
	rows := modelUsageForTelemetry(sessionUsageStats{CostLedger: l})
	if len(rows) != 1 || rows[0].Model != "" || rows[0].Tokens != 300 {
		t.Fatalf("legacy=%+v", rows)
	}
}
