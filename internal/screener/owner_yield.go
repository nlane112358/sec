package screener

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"text/tabwriter"

	"sec/internal/store"
)

// DefaultHurdleRate represents the 10-Year Treasury Yield (4.84%) + 2.0% equity hurdle rate (6.84%).
const DefaultHurdleRate = 0.0684

// OwnerYieldCandidate represents a company ranked by its Intangible-Adjusted Owner Earnings Yield.
type OwnerYieldCandidate struct {
	Rank                  int     `json:"rank"`
	Ticker                string  `json:"ticker"`
	Company               string  `json:"company"`
	SIC                   string  `json:"sic"`
	SICDescription        string  `json:"sic_description"`
	Sector                string  `json:"sector"`
	Currency              string  `json:"currency"`
	Price                 float64 `json:"price"`
	MarketCap             float64 `json:"market_cap"`
	HasMarketCap          bool    `json:"has_market_cap"`
	OwnerYield            float64 `json:"owner_yield"`             // Intangible-Adjusted Owner Earnings / Market Cap
	StandardOwnerYield    float64 `json:"standard_owner_yield"`    // (FCF - SBC) / Market Cap
	FCFYield              float64 `json:"fcf_yield"`               // Reported FCF / Market Cap
	YieldSpread           float64 `json:"yield_spread"`            // FCF Yield - Standard Owner Yield (SBC Dilution Drag)
	OwnerEarnings         float64 `json:"owner_earnings"`          // FCF - SBC Proxy
	AdjustedOwnerEarnings float64 `json:"adjusted_owner_earnings"` // (FCF - SBC) + Net Intangible Investment
	FCF                   float64 `json:"fcf"`                     // Free Cash Flow
	SBC                   float64 `json:"sbc"`                     // Stock-Based Compensation
	SBCPercentOfFCF       float64 `json:"sbc_percent_of_fcf"`      // SBC / FCF * 100
	AdjustedROIC          float64 `json:"adjusted_roic"`
	HasAdjustedROIC       bool    `json:"has_adjusted_roic"`
	GAAPROIC              float64 `json:"gaap_roic"`
	HasGAAPROIC           bool    `json:"has_gaap_roic"`
	GAAP_PE               float64 `json:"gaap_pe"`
	HasGAAP_PE            bool    `json:"has_gaap_pe"`
	Revenue               float64 `json:"revenue"`
	OperatingMargin       float64 `json:"operating_margin"`
	HurdleRate            float64 `json:"hurdle_rate"`
	BeatsHurdle           bool    `json:"beats_hurdle"`
	YieldTier             string  `json:"yield_tier"`
	ArbitrageSignal       string  `json:"arbitrage_signal"`
}

// OwnerYieldFilter controls filtering and sorting parameters for owner yield ranking.
type OwnerYieldFilter struct {
	MinYield          float64 // Minimum Intangible Owner Yield (e.g. 0.05 for 5%)
	MinAdjustedROIC   float64 // Minimum Intangible-Adjusted ROIC (e.g. 0.10 for 10%)
	MinRevenue        float64 // Minimum annual revenue
	Years             int     // Period depth (default 5)
	RequireHurdleBeat bool    // Require OwnerYield >= HurdleRate
	IncludeFinancials bool    // Include SIC 6000-6799
	SortBy            string  // "yield", "standard_yield", "fcf_yield", "roic", "market_cap", "price", "owner_earnings", "pe", "spread"
	SortAsc           bool    // Sort in ascending order instead of descending
}

// OwnerYieldResult returns ranked candidates and universe-wide yield metrics.
type OwnerYieldResult struct {
	Candidates  []OwnerYieldCandidate `json:"candidates"`
	Total       int                   `json:"total"`
	HurdleRate  float64               `json:"hurdle_rate"`
	MedianYield float64               `json:"median_yield"`
	TopYield    float64               `json:"top_yield"`
	BeatsCount  int                   `json:"beats_count"`
}

// ScreenOwnerYield screens and ranks companies by Owner Earnings Yield ((FCF - SBC + Net Intangibles) / Market Cap).
func ScreenOwnerYield(
	ctx context.Context,
	s *store.Store,
	tickers []string,
	marketCaps, prices map[string]float64,
	filter OwnerYieldFilter,
) (OwnerYieldResult, error) {
	var result OwnerYieldResult
	result.HurdleRate = DefaultHurdleRate

	if filter.Years < 5 || filter.Years > 6 {
		filter.Years = 5
	}
	if filter.MinRevenue < 0 || math.IsNaN(filter.MinRevenue) || math.IsInf(filter.MinRevenue, 0) {
		filter.MinRevenue = 50_000_000
	}

	cleanTickers := NormalizeTickers(tickers)
	if len(tickers) > 0 && len(cleanTickers) == 0 {
		return result, nil
	}

	// Ensure precomputed variables are available
	if _, err := PrecomputeMissingOrDirty(ctx, s, PrecomputeOptions{
		Tickers:   cleanTickers,
		RankYears: []int{filter.Years},
		ArbYears:  []int{filter.Years},
	}); err != nil {
		return result, err
	}

	arbRows, err := s.LoadGAAPArbitrageVariables(ctx, filter.Years, GAAPArbitrageSchemaVersion, cleanTickers)
	if err != nil {
		return result, err
	}

	rankRows, err := s.LoadRankVariables(ctx, filter.Years, RankSchemaVersion, cleanTickers)
	if err != nil {
		return result, err
	}

	rankMap := make(map[string]store.RankVariableRecord, len(rankRows))
	for _, r := range rankRows {
		rankMap[strings.ToUpper(strings.TrimSpace(r.Ticker))] = r
	}

	candidates := make([]OwnerYieldCandidate, 0, len(arbRows))

	for _, row := range arbRows {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if !row.Eligible {
			continue
		}
		if !filter.IncludeFinancials && row.Sector == "Financials" {
			continue
		}
		if filter.MinRevenue > 0 && row.GAAPRevenue < filter.MinRevenue {
			continue
		}
		if !row.HasOwnerEarnings {
			continue
		}

		marketCap := resolveMarketCapFromRecord(row, marketCaps, prices)
		price := valuationForTicker(prices, row.Ticker)
		if price <= 0 && marketCap > 0 && row.SharesOutstanding > 0 {
			price = marketCap / row.SharesOutstanding
		}
		if price > 0 && marketCap <= 0 && row.SharesOutstanding > 0 {
			marketCap = price * row.SharesOutstanding
		}

		// Filter out records where market cap or share counts are invalid / microcap float artifacts (<10k shares or <$10M cap)
		if !finite(marketCap) || marketCap < 10_000_000 || !finite(price) || price <= 0 || row.SharesOutstanding < 10_000 {
			continue
		}

		ownerYield := row.AdjustedOwnerEarnings / marketCap
		if !finite(ownerYield) {
			continue
		}
		standardOwnerYield := row.OwnerEarnings / marketCap
		if !finite(standardOwnerYield) {
			standardOwnerYield = 0
		}

		// FCF and SBC calculations
		rankRow, hasRank := rankMap[strings.ToUpper(row.Ticker)]
		fcf := 0.0
		sbc := 0.0
		fcfYield := 0.0
		if hasRank && rankRow.Revenue > 0 {
			fcf = rankRow.FCFMargin * rankRow.Revenue
			sbc = (rankRow.FCFMargin - rankRow.OwnerEarningsMargin) * rankRow.Revenue
			if sbc < 0 {
				sbc = 0
			}
			fcfYield = fcf / marketCap
		} else {
			fcf = row.OwnerEarnings
			sbc = 0
			fcfYield = standardOwnerYield
		}

		sbcPercentOfFCF := 0.0
		if fcf > 0 && sbc > 0 {
			sbcPercentOfFCF = (sbc / fcf) * 100
		}
		yieldSpread := math.Max(0, fcfYield-standardOwnerYield)

		beatsHurdle := ownerYield >= result.HurdleRate

		// Filters
		if filter.MinYield > 0 && ownerYield < filter.MinYield {
			continue
		}
		if filter.RequireHurdleBeat && !beatsHurdle {
			continue
		}
		if filter.MinAdjustedROIC > 0 && (!row.HasAdjustedROIC || row.AdjustedROIC < filter.MinAdjustedROIC) {
			continue
		}

		// Categorize yield tier
		yieldTier := "Negative Yield"
		switch {
		case ownerYield >= 0.08:
			yieldTier = "Franchise (≥8%)"
		case beatsHurdle:
			yieldTier = "Hurdle Beat (≥6.84%)"
		case ownerYield >= 0.04:
			yieldTier = "Moderate (4–6.8%)"
		case ownerYield > 0:
			yieldTier = "Low (<4%)"
		}

		gaapPE := 0.0
		hasGAAP_PE := false
		if row.GAAPNetIncome > 0 && marketCap > 0 {
			gaapPE = marketCap / row.GAAPNetIncome
			hasGAAP_PE = true
		}

		cand := OwnerYieldCandidate{
			Ticker:                row.Ticker,
			Company:               row.CompanyName,
			SIC:                   row.SIC,
			SICDescription:        row.SICDescription,
			Sector:                row.Sector,
			Currency:              row.Currency,
			Price:                 price,
			MarketCap:             marketCap,
			HasMarketCap:          true,
			OwnerYield:            ownerYield,
			StandardOwnerYield:    standardOwnerYield,
			FCFYield:              fcfYield,
			YieldSpread:           yieldSpread,
			OwnerEarnings:         row.OwnerEarnings,
			AdjustedOwnerEarnings: row.AdjustedOwnerEarnings,
			FCF:                   fcf,
			SBC:                   sbc,
			SBCPercentOfFCF:       sbcPercentOfFCF,
			AdjustedROIC:          row.AdjustedROIC,
			HasAdjustedROIC:       row.HasAdjustedROIC,
			GAAPROIC:              row.GAAPROIC,
			HasGAAPROIC:           row.HasGAAPROIC,
			GAAP_PE:               gaapPE,
			HasGAAP_PE:            hasGAAP_PE,
			Revenue:               row.GAAPRevenue,
			OperatingMargin:       row.GAAPOperatingMargin,
			HurdleRate:            result.HurdleRate,
			BeatsHurdle:           beatsHurdle,
			YieldTier:             yieldTier,
			ArbitrageSignal:       row.BaseArbitrageSignal,
		}

		candidates = append(candidates, cand)
	}

	// Sort candidates
	sortBy := strings.ToLower(strings.TrimSpace(filter.SortBy))
	if sortBy == "" {
		sortBy = "yield"
	}

	sort.Slice(candidates, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "standard_yield":
			less = candidates[i].StandardOwnerYield < candidates[j].StandardOwnerYield
		case "fcf_yield":
			less = candidates[i].FCFYield < candidates[j].FCFYield
		case "roic":
			less = candidates[i].AdjustedROIC < candidates[j].AdjustedROIC
		case "market_cap":
			less = candidates[i].MarketCap < candidates[j].MarketCap
		case "price":
			less = candidates[i].Price < candidates[j].Price
		case "owner_earnings":
			less = candidates[i].AdjustedOwnerEarnings < candidates[j].AdjustedOwnerEarnings
		case "pe":
			if candidates[i].HasGAAP_PE != candidates[j].HasGAAP_PE {
				less = !candidates[i].HasGAAP_PE
			} else {
				less = candidates[i].GAAP_PE < candidates[j].GAAP_PE
			}
		case "spread", "sbc_drag":
			less = candidates[i].YieldSpread < candidates[j].YieldSpread
		default: // "yield"
			less = candidates[i].OwnerYield < candidates[j].OwnerYield
		}

		if filter.SortAsc {
			return less
		}
		return !less
	})

	beatsCount := 0
	yields := make([]float64, len(candidates))
	for i := range candidates {
		candidates[i].Rank = i + 1
		yields[i] = candidates[i].OwnerYield
		if candidates[i].BeatsHurdle {
			beatsCount++
		}
	}

	result.Candidates = candidates
	result.Total = len(candidates)
	result.BeatsCount = beatsCount

	if len(yields) > 0 {
		result.TopYield = yields[0]
		// Calculate median
		sortedYields := append([]float64(nil), yields...)
		sort.Float64s(sortedYields)
		mid := len(sortedYields) / 2
		if len(sortedYields)%2 == 0 {
			result.MedianYield = (sortedYields[mid-1] + sortedYields[mid]) / 2
		} else {
			result.MedianYield = sortedYields[mid]
		}
	}

	return result, nil
}

// WriteOwnerYieldTable prints owner-yield screening candidates in a formatted table.
func WriteOwnerYieldTable(output io.Writer, result OwnerYieldResult) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "RANK\tTICKER\tCOMPANY\tPRICE\tMARKET CAP\tOWNER YLD\tSTD OWNER YLD\tFCF YLD\tSBC/FCF\tADJ ROIC\tTIER\tHURDLE"); err != nil {
		return err
	}
	for _, c := range result.Candidates {
		companyName := c.Company
		if len(companyName) > 24 {
			companyName = strings.TrimSpace(companyName[:23]) + "…"
		}
		peOrPrice := fmt.Sprintf("$%.2f", c.Price)
		mcapCell := formatSummaryValue(c.MarketCap, c.Currency)
		sbcPct := "0.0%"
		if c.SBCPercentOfFCF > 0 {
			sbcPct = fmt.Sprintf("%.1f%%", c.SBCPercentOfFCF)
		}
		adjROIC := formatPercent(c.AdjustedROIC, c.HasAdjustedROIC)
		hurdle := "no"
		if c.BeatsHurdle {
			hurdle = "PASS"
		}
		if _, err := fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%s\t%.2f%%\t%.2f%%\t%.2f%%\t%s\t%s\t%s\t%s\n",
			c.Rank, c.Ticker, companyName, peOrPrice, mcapCell,
			c.OwnerYield*100, c.StandardOwnerYield*100, c.FCFYield*100,
			sbcPct, adjROIC, c.YieldTier, hurdle); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "\nOwner Yield = (FCF − SBC + Net Intangible Investment) / Market Cap · Hurdle Rate: %.2f%% (10Y Treasury + 2.0%%)\n", result.HurdleRate*100)
	return err
}
