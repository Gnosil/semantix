package stats

import (
	"context"
	"testing"
	"time"

	"semantix/harness/billing"
	"semantix/harness/event"
	"semantix/harness/provider"
)

func TestSavingsRestoresDiscountsAndDedupeMaintenance(t *testing.T) {
	w := NewWriter(t.TempDir())
	now := time.Now()
	records := []record{
		{Timestamp: now, Total: 1000, Requests: 1, CacheReadDiscountComplete: true, CacheReadDiscounts: []billing.Money{{Amount: "2.592", Currency: "USD"}}},
		{Timestamp: now, Total: 1000, Requests: 1, CacheReadDiscountComplete: true, CacheReadDiscounts: []billing.Money{{Amount: "5", Currency: "CNY"}}},
		{Timestamp: now, Total: 1000}, // legacy cost remains unknown
		{Timestamp: now, Kind: "context_reduction", MaintenanceID: "op-1", ReducedTokens: 48600},
		{Timestamp: now.Add(24 * time.Hour), Kind: "context_reduction", MaintenanceID: "op-1", ReducedTokens: 48600},
		{Timestamp: now, Kind: "memory_recall", MemoryHits: 3},
		{Timestamp: now, Kind: "memory_recall", MemoryHits: 0},
	}
	for _, rec := range records {
		if err := w.Append(rec); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewWriter(w.dir).Savings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests != 3 || got.PricedRequests != 2 || got.DiscountComplete || got.ReducedTokens != 48600 || got.MaintenanceOperations != 1 || got.MemoryHits != 3 || got.Recalls != 2 {
		t.Fatalf("totals = %+v", got)
	}
	if len(got.Discounts) != 2 || got.Discounts[0].Amount != "5" || got.Discounts[1].Amount != "2.592" {
		t.Fatalf("currencies = %+v", got.Discounts)
	}
	usage, err := w.queryJSONL(SourceFilter{From: now.AddDate(0, 0, -1), To: now.AddDate(0, 0, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if usage.Requests != 3 || usage.Tokens != 3000 || usage.Turns != 0 {
		t.Fatalf("activity polluted usage: %+v", usage)
	}
}

func TestSavingsRecorderUsesRealReceiptsAndServedRecall(t *testing.T) {
	w := NewWriter(t.TempDir())
	r := &Recorder{writer: w, dispatcher: dispatcherFor(w), source: "desktop"}
	u := &provider.Usage{PromptTokens: 1000000, TotalTokens: 1000000, CacheHitTokens: 1000000, RequestCount: 1}
	q := billing.BuildQuote(billing.QuoteInput{Usage: billing.UsageTokens{PromptTokens: 1000000, CacheHitTokens: 1000000}, Rates: billing.RateCard{Input: 3, CacheHit: .3, Currency: "USD"}})
	r.Emit(event.Event{Kind: event.Usage, Usage: u, CostQuote: &q})
	r.Emit(event.Event{Kind: event.ContextMaintenanceEvent, Maintenance: &event.ContextMaintenance{Status: "applied", OperationID: "receipt", SavedTokens: 400}})
	r.Emit(event.Event{Kind: event.ContextMaintenanceEvent, Maintenance: &event.ContextMaintenance{Status: "planned", OperationID: "planned", SavedTokens: 800}})
	r.RecordMemoryRecall(event.MemoryRecallAudit{Hits: []event.MemoryRecallHit{{ID: "fact"}}})
	r.RecordMemoryRecall(event.MemoryRecallAudit{Hits: []event.MemoryRecallHit{{ID: "not-served"}}, Suppressed: "budget"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := w.Savings()
	if err != nil {
		t.Fatal(err)
	}
	if !got.DiscountComplete || got.Discounts[0].Amount != "2.7" || got.ReducedTokens != 400 || got.MemoryHits != 1 || got.Recalls != 2 {
		t.Fatalf("recorded = %+v", got)
	}
}
