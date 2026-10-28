package billing

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func discountQuote(hit int, input, cache float64, currency string) CostQuote {
	return BuildQuote(QuoteInput{Usage: UsageTokens{PromptTokens: hit + 100, CacheHitTokens: hit, CacheMissTokens: 100}, Rates: RateCard{Input: input, CacheHit: cache, Currency: currency}, ModelRef: "example", UsageSource: "executor"})
}

func TestCacheReadDiscountOccurrenceRatesAndCurrency(t *testing.T) {
	q := discountQuote(960000, 3, .3, "USD")
	if !q.CacheReadDiscountComplete || len(q.CacheReadDiscounts) != 1 || q.CacheReadDiscounts[0].Amount != "2.592" {
		t.Fatalf("discount = %+v", q)
	}
	changedRate := discountQuote(1000000, 5, 1, "USD")
	cny := discountQuote(1000000, 7, 2, "CNY")
	total := AggregateQuotes([]CostQuote{q, changedRate, cny}, "USD")
	if !total.CacheReadDiscountComplete || len(total.CacheReadDiscounts) != 2 || total.CacheReadDiscounts[0].Amount != "5" || total.CacheReadDiscounts[1].Amount != "6.592" {
		t.Fatalf("mixed total = %+v", total.CacheReadDiscounts)
	}
}

func TestCacheReadDiscountMissingAndLegacyRemainUnknown(t *testing.T) {
	for _, q := range []CostQuote{discountQuote(100, 0, 0, "USD"), discountQuote(100, 3, .3, ""), discountQuote(100, math.NaN(), .3, "USD"), discountQuote(100, 3, math.Inf(1), "USD")} {
		if q.CacheReadDiscountComplete || len(q.CacheReadDiscounts) > 0 {
			t.Fatalf("unknown has discount %+v", q)
		}
	}
	q := discountQuote(100, 3, .3, "USD")
	total := AggregateQuotes([]CostQuote{q, {Original: MoneyOf(Zero, "USD"), CostComplete: true}}, "USD")
	if total.CacheReadDiscountComplete {
		t.Fatal("legacy quote must not make a complete saving")
	}
}

func TestCacheReadDiscountLedgerRestoreNoDoubleCounting(t *testing.T) {
	l := NewLedger()
	q := discountQuote(1000000, 3, .3, "USD")
	for range 2 {
		l.Add(q, UsageTokens{PromptTokens: 1000100, CacheHitTokens: 1000000}, time.Now())
	}
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	var restored Ledger
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	total := restored.Total("CNY")
	if !total.CacheReadDiscountComplete || len(total.CacheReadDiscounts) != 1 || total.CacheReadDiscounts[0].Amount != "5.4" {
		t.Fatalf("restored = %+v", total.CacheReadDiscounts)
	}
}

func TestCacheReadDiscountDoesNotCountCacheWritesOrHidePremium(t *testing.T) {
	in := QuoteInput{Usage: UsageTokens{PromptTokens: 2000, CacheHitTokens: 1000, CacheMissTokens: 1000, CacheWriteTokens: 1000, CacheWriteBilledTokens: 1250}, Rates: RateCard{Input: 3, CacheHit: .3, Currency: "USD"}}
	q := BuildQuote(in)
	if q.CacheReadDiscounts[0].Amount != "0.0027" {
		t.Fatalf("write premium included: %+v", q)
	}
	q = discountQuote(1000000, 3, 4, "USD")
	if q.CacheReadDiscounts[0].Amount != "-1" {
		t.Fatal("negative discount must not be clamped")
	}
}
