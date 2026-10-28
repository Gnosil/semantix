package billing

import (
	"math"
	"sort"
)

func cacheReadDiscount(in QuoteInput, costComplete bool) ([]Money, bool) {
	r := in.Rates
	// A missing input price/currency cannot establish a counterfactual. A zero
	// cache rate is valid (free cached reads); a zero input rate is ambiguous.
	if !costComplete || !usageHasFacts(in.Usage) || r.Input <= 0 || r.CacheHit < 0 ||
		math.IsNaN(r.Input) || math.IsInf(r.Input, 0) || math.IsNaN(r.CacheHit) || math.IsInf(r.CacheHit, 0) ||
		NormalizeCurrency(r.Currency) == "" || in.Usage.CacheHitTokens < 0 {
		return nil, false
	}
	amount := NewAmountFromFloat(float64(in.Usage.CacheHitTokens) * (r.Input - r.CacheHit) / 1e6)
	return []Money{MoneyOf(amount, NormalizeCurrency(r.Currency))}, true
}

// Preserve original currencies and completeness across old/new quotes. Never
// reprice historical calls using today's model or add different currencies.
func mergeCacheReadDiscounts(quotes ...CostQuote) ([]Money, bool) {
	complete := len(quotes) > 0
	totals := map[string]Amount{}
	for _, q := range quotes {
		if !q.CacheReadDiscountComplete || len(q.CacheReadDiscounts) == 0 {
			complete = false
		}
		for _, m := range q.CacheReadDiscounts {
			currency := NormalizeCurrency(m.Currency)
			amount, err := ParseAmount(m.Amount)
			if currency == "" || err != nil {
				complete = false
				continue
			}
			totals[currency] = totals[currency].Add(amount)
		}
	}
	codes := make([]string, 0, len(totals))
	for code := range totals {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	var result []Money
	for _, code := range codes {
		result = append(result, MoneyOf(totals[code], code))
	}
	return result, complete
}
