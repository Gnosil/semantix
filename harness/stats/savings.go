package stats

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"semantix/harness/billing"
)

// SavingsTotals reads recorded activity across entry points. Discounts retain
// occurrence-time rates and original currencies; legacy unknowns stay unknown.
// Context reductions and recall matches begin when their recording is enabled.
type SavingsTotals struct {
	Tokens                 int64           `json:"tokens"`
	InputTokens            int64           `json:"inputTokens"`
	OutputTokens           int64           `json:"outputTokens"`
	Models                 []ModelUsage    `json:"models"`
	Discounts              []billing.Money `json:"discounts"`
	DiscountComplete       bool            `json:"discountComplete"`
	PricedRequests         int             `json:"pricedRequests"`
	Requests               int             `json:"requests"`
	CacheHit               int64           `json:"cacheHit"`
	CacheMiss              int64           `json:"cacheMiss"`
	ReducedTokens          int64           `json:"reducedTokens"`
	MaintenanceOperations  int             `json:"maintenanceOperations"`
	MemoryHits             int64           `json:"memoryHits"`
	Recalls                int             `json:"recalls"`
	Since                  string          `json:"since,omitempty"`
	SubscriptionEquivalent bool            `json:"subscriptionEquivalent,omitempty"`
}

func (w *Writer) Savings() (SavingsTotals, error) {
	out := SavingsTotals{Discounts: []billing.Money{}, Models: []ModelUsage{}}
	if w == nil || strings.TrimSpace(w.dir) == "" {
		return out, nil
	}
	files, err := os.ReadDir(w.dir)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	amounts := map[string]billing.Amount{}
	seenMaintenance := map[string]bool{}
	modelTokens := map[string]int64{}
	modelRequests := map[string]int{}
	var since time.Time
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
			continue
		}
		day := strings.TrimSuffix(file.Name(), ".jsonl")
		if _, err := time.Parse(dayLayout, day); err != nil {
			continue
		}
		recs, err := readDaily(w.dir, day)
		if err != nil {
			return out, err
		}
		for _, rec := range recs {
			tracked := false
			switch rec.Kind {
			case "context_reduction":
				if rec.MaintenanceID != "" && rec.ReducedTokens > 0 && !seenMaintenance[rec.MaintenanceID] {
					seenMaintenance[rec.MaintenanceID] = true
					out.ReducedTokens += int64(rec.ReducedTokens)
					out.MaintenanceOperations++
					tracked = true
				}
			case "memory_recall":
				out.MemoryHits += int64(max(0, rec.MemoryHits))
				out.Recalls++
				tracked = true
			default:
				if rec.Turn {
					continue
				}
				requests := rec.Requests
				if requests <= 0 && rec.Total > 0 {
					requests = 1
				}
				if requests <= 0 {
					continue
				}
				out.Requests += requests
				tracked = true
				out.Tokens += int64(max(0, rec.Total))
				out.InputTokens += int64(max(0, rec.Prompt))
				out.OutputTokens += int64(max(0, rec.Completion))
				modelTokens[rec.ModelRef] += int64(max(0, rec.Total))
				modelRequests[rec.ModelRef] += requests
				out.CacheHit += int64(max(0, rec.CacheHit))
				out.CacheMiss += int64(max(0, rec.CacheMiss))
				valid := rec.CacheReadDiscountComplete && len(rec.CacheReadDiscounts) > 0
				pending := map[string]billing.Amount{}
				for _, m := range rec.CacheReadDiscounts {
					amount, err := billing.ParseAmount(m.Amount)
					currency := billing.NormalizeCurrency(m.Currency)
					if err != nil || currency == "" {
						valid = false
						break
					}
					pending[currency] = pending[currency].Add(amount)
				}
				if valid {
					for currency, amount := range pending {
						amounts[currency] = amounts[currency].Add(amount)
					}
					out.PricedRequests += requests
					tracked = true
					out.SubscriptionEquivalent = out.SubscriptionEquivalent || rec.BillingMode == billing.BillingModeSubscriptionEquivalent
				}
			}
			if tracked && !rec.Timestamp.IsZero() && (since.IsZero() || rec.Timestamp.Before(since)) {
				since = rec.Timestamp
			}
		}
	}
	currencies := make([]string, 0, len(amounts))
	for currency := range amounts {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)
	for _, currency := range currencies {
		out.Discounts = append(out.Discounts, billing.MoneyOf(amounts[currency], currency))
	}
	out.DiscountComplete = out.Requests > 0 && out.PricedRequests == out.Requests
	if !since.IsZero() {
		out.Since = since.Format(dayLayout)
	}
	out.Models = modelsSorted(modelTokens)
	for i := range out.Models {
		out.Models[i].Requests = modelRequests[out.Models[i].Model]
		if out.Tokens > 0 {
			out.Models[i].Percent = float64(out.Models[i].Tokens) / float64(out.Tokens) * 100
		}
	}
	return out, nil
}
