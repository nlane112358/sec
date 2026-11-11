package screener

import (
	"context"
	"fmt"
	"math"
	"strings"

	"sec/internal/fundamentals"
	"sec/internal/market"
	"sec/internal/store"
)

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func valuationForTicker(values map[string]float64, ticker string) float64 {
	if value, ok := values[strings.ToUpper(strings.TrimSpace(ticker))]; ok {
		return value
	}
	for key, value := range values {
		if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(ticker)) {
			return value
		}
	}
	return 0
}

func formatPercent(value float64, present bool) string {
	if !present || !finite(value) {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", value*100)
}

func formatSummaryValue(val float64, unit string) string {
	u := strings.TrimSpace(unit)
	if u == "" {
		u = "USD"
	}
	abs := math.Abs(val)
	sign := ""
	if val < 0 {
		sign = "-"
	}
	if strings.EqualFold(u, "shares") {
		switch {
		case abs >= 1e9:
			return fmt.Sprintf("%s%.2fB shares", sign, abs/1e9)
		case abs >= 1e6:
			return fmt.Sprintf("%s%.2fM shares", sign, abs/1e6)
		case abs >= 1e3:
			return fmt.Sprintf("%s%.2fK shares", sign, abs/1e3)
		default:
			return fmt.Sprintf("%s%.0f shares", sign, abs)
		}
	}
	switch {
	case abs >= 1e12:
		return fmt.Sprintf("%s%.2fT %s", sign, abs/1e12, u)
	case abs >= 1e9:
		return fmt.Sprintf("%s%.2fB %s", sign, abs/1e9, u)
	case abs >= 1e6:
		return fmt.Sprintf("%s%.2fM %s", sign, abs/1e6, u)
	case abs >= 1e3:
		return fmt.Sprintf("%s%.2fK %s", sign, abs/1e3, u)
	default:
		return fmt.Sprintf("%s%.2f %s", sign, abs, u)
	}
}

// ResolveMarketCap resolves market capitalization for a company, applying ADR conversion
// if applicable and converting to the company's reporting currency.
func ResolveMarketCap(s *store.Store, ctx context.Context, summary fundamentals.FinancialSummary, ticker string, marketCaps, prices map[string]float64) float64 {
	marketCap := valuationForTicker(marketCaps, ticker)
	if !finite(marketCap) || marketCap <= 0 {
		price := valuationForTicker(prices, ticker)
		if finite(price) && price > 0 {
			var shareCount float64
			for _, metricName := range []string{"Shares Outstanding", "Diluted Weighted Average Shares"} {
				values := fundamentals.NamedMetric(summary, metricName)
				for index := 0; index < len(summary.Periods) && index < len(values); index++ {
					value := values[index]
					if fundamentals.IsShareCount(value) && finite(value.Value) && value.Value > 0 {
						shareCount = value.Value
						break
					}
				}
				if shareCount > 0 {
					break
				}
			}
			if shareCount <= 0 {
				if databaseShares, ok := s.LatestShareCount(ctx, ticker); ok {
					shareCount = databaseShares
				}
			}
			if shareCount > 0 {
				adrShares := market.OrdinaryToADRShares(ticker, shareCount)
				marketCapUSD := price * adrShares
				currency := fundamentals.SummaryCurrency(summary)
				if currency != "" && !strings.EqualFold(currency, "USD") {
					marketCap = market.FromUSD(marketCapUSD, currency)
				} else {
					marketCap = marketCapUSD
				}
			}
		}
	}
	return marketCap
}
