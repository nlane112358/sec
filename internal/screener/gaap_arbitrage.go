package screener

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"sec/internal/fundamentals"
	"sec/internal/store"
)

type ModernGAAPCandidate struct {
	Ticker                string  `json:"ticker"`
	Company               string  `json:"company"`
	SIC                   string  `json:"sic"`
	SICDescription        string  `json:"sic_description"`
	Currency              string  `json:"currency"`
	MarketCap             float64 `json:"market_cap"`
	HasMarketCap          bool    `json:"has_market_cap"`
	GAAPRevenue           float64 `json:"gaap_revenue"`
	GAAPNetIncome         float64 `json:"gaap_net_income"`
	GAAPOperatingIncome   float64 `json:"gaap_operating_income"`
	GAAPOperatingMargin   float64 `json:"gaap_operating_margin"`
	GAAPROIC              float64 `json:"gaap_roic"`
	HasGAAPROIC           bool    `json:"has_gaap_roic"`
	GAAP_PE               float64 `json:"gaap_pe"`
	HasGAAP_PE            bool    `json:"has_gaap_pe"`
	GAAPUnprofitable      bool    `json:"gaap_unprofitable"`
	IntangibleCapital     float64 `json:"intangible_capital"`
	AdjustedEBIT          float64 `json:"adjusted_ebit"`
	AdjustedEBITMargin    float64 `json:"adjusted_ebit_margin"`
	AdjustedInvestedCap   float64 `json:"adjusted_invested_cap"`
	AdjustedROIC          float64 `json:"adjusted_roic"`
	HasAdjustedROIC       bool    `json:"has_adjusted_roic"`
	AvgAdjustedROIC       float64 `json:"avg_adjusted_roic"`
	ROICDelta             float64 `json:"roic_delta"`
	SurgingROIC           bool    `json:"surging_roic"`
	OwnerEarnings         float64 `json:"owner_earnings"`
	AdjustedOwnerEarnings float64 `json:"adjusted_owner_earnings"`
	OwnerEarningsYield    float64 `json:"owner_earnings_yield"`
	HasOwnerEarningsYield bool    `json:"has_owner_earnings_yield"`
	ArbitrageScore        float64 `json:"arbitrage_score"`
	ArbitrageSignal       string  `json:"arbitrage_signal"`
}

type ModernGAAPFilter struct {
	MinAdjustedROIC      float64
	MinOwnerYield        float64
	MinRevenue           float64
	Years                int
	RequireSurging       bool
	RequireGAAPExpensive bool
	MaxGAAPROIC          float64
	IncludeFinancials    bool
	SortBy               string
}

// ScreenModernGAAPArbitrage screens companies using the Peters & Taylor / Damodaran
// intangible capitalization model, identifying disconnects between GAAP ROIC / P/E
// and Intangible-Adjusted ROIC and Owner Earnings Yield.
func ScreenModernGAAPArbitrage(ctx context.Context, s *store.Store, tickers []string, marketCaps, prices map[string]float64, filter ModernGAAPFilter) ([]ModernGAAPCandidate, error) {
	if filter.Years < 1 {
		filter.Years = 5
	}
	if filter.MinRevenue <= 0 {
		filter.MinRevenue = 100_000_000
	}

	var companies []store.LocalCompany
	if len(tickers) > 0 {
		tickerSet := make(map[string]bool)
		for _, t := range tickers {
			tickerSet[strings.ToUpper(strings.TrimSpace(t))] = true
		}
		all, err := s.Companies(ctx)
		if err != nil {
			return nil, err
		}
		for _, c := range all {
			if tickerSet[strings.ToUpper(c.Ticker)] {
				companies = append(companies, c)
			}
		}
	} else {
		all, err := s.Companies(ctx)
		if err != nil {
			return nil, err
		}
		companies = all
	}

	type resultItem struct {
		cand ModernGAAPCandidate
		ok   bool
	}

	numWorkers := 8
	jobs := make(chan store.LocalCompany, len(companies))
	results := make(chan resultItem, len(companies))

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for comp := range jobs {
				if ctx.Err() != nil {
					return
				}
				cand, ok := evaluateModernGAAPCompany(ctx, s, comp, filter, marketCaps, prices)
				if ok {
					results <- resultItem{cand: cand, ok: true}
				}
			}
		}()
	}

	for _, company := range companies {
		if ctx.Err() != nil {
			close(jobs)
			wg.Wait()
			close(results)
			return nil, ctx.Err()
		}
		jobs <- company
	}
	close(jobs)
	wg.Wait()
	close(results)

	candidates := make([]ModernGAAPCandidate, 0)
	for r := range results {
		if r.ok {
			candidates = append(candidates, r.cand)
		}
	}

	// Sort candidates
	switch strings.ToLower(strings.TrimSpace(filter.SortBy)) {
	case "roic":
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].AdjustedROIC > candidates[j].AdjustedROIC
		})
	case "spread", "delta":
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].ROICDelta > candidates[j].ROICDelta
		})
	case "yield":
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].HasOwnerEarningsYield != candidates[j].HasOwnerEarningsYield {
				return candidates[i].HasOwnerEarningsYield
			}
			if candidates[i].OwnerEarningsYield != candidates[j].OwnerEarningsYield {
				return candidates[i].OwnerEarningsYield > candidates[j].OwnerEarningsYield
			}
			return candidates[i].AdjustedROIC > candidates[j].AdjustedROIC
		})
	default:
		sort.Slice(candidates, func(i, j int) bool {
			if filter.MinOwnerYield > 0 {
				if candidates[i].HasOwnerEarningsYield != candidates[j].HasOwnerEarningsYield {
					return candidates[i].HasOwnerEarningsYield
				}
				if candidates[i].OwnerEarningsYield != candidates[j].OwnerEarningsYield {
					return candidates[i].OwnerEarningsYield > candidates[j].OwnerEarningsYield
				}
			}
			return candidates[i].ArbitrageScore > candidates[j].ArbitrageScore
		})
	}

	return candidates, nil
}

func evaluateModernGAAPCompany(ctx context.Context, s *store.Store, company store.LocalCompany, filter ModernGAAPFilter, marketCaps, prices map[string]float64) (ModernGAAPCandidate, bool) {
	var zero ModernGAAPCandidate
	if !filter.IncludeFinancials && sectorFromSIC(company.SIC) == "Financials" {
		return zero, false
	}

	summary, err := fundamentals.LoadFinancialSummary(ctx, s, filter.Years, company.Ticker)
	if err != nil || len(summary.Periods) == 0 {
		return zero, false
	}

	currency := fundamentals.SummaryCurrency(summary)
	if currency == "" {
		currency = "USD"
	}

	revenueMetric := fundamentals.NamedMetric(summary, "Revenue")
	if len(revenueMetric) == 0 || !revenueMetric[0].Present || revenueMetric[0].Value < filter.MinRevenue {
		return zero, false
	}

	intCapMetric := fundamentals.NamedMetric(summary, "Intangible Capital")
	adjEBITMetric := fundamentals.NamedMetric(summary, "Adjusted Operating Income")
	adjROICMetric := fundamentals.NamedMetric(summary, "Intangible-Adjusted ROIC")
	amortMetric := fundamentals.NamedMetric(summary, "Amortization of Intangibles")

	if len(intCapMetric) == 0 || !intCapMetric[0].Present || intCapMetric[0].Value <= 0 {
		return zero, false
	}
	if len(adjEBITMetric) == 0 || !adjEBITMetric[0].Present || adjEBITMetric[0].Value <= 0 {
		return zero, false
	}

	// Standard GAAP metrics
	opIncomeMetric := fundamentals.NamedMetric(summary, "Operating Income")
	netIncomeMetric := fundamentals.NamedMetric(summary, "Net Income")
	equityMetric := fundamentals.NamedMetric(summary, "Stockholders' Equity")
	debtMetric := fundamentals.NamedMetric(summary, "Debt / Borrowings")
	cashMetric := fundamentals.NamedMetric(summary, "Cash & Equivalents")
	taxMetric := fundamentals.NamedMetric(summary, "Income Tax Expense")
	pretaxMetric := fundamentals.NamedMetric(summary, "Income Before Tax")

	gaapRevenue := revenueMetric[0].Value
	gaapOpIncome := 0.0
	if len(opIncomeMetric) > 0 && opIncomeMetric[0].Present {
		gaapOpIncome = opIncomeMetric[0].Value
	}
	gaapNetIncome := 0.0
	if len(netIncomeMetric) > 0 && netIncomeMetric[0].Present {
		gaapNetIncome = netIncomeMetric[0].Value
	}
	gaapOpMargin := gaapOpIncome / gaapRevenue
	gaapUnprofitable := gaapNetIncome <= 0 || gaapOpIncome <= 0

	gaapROIC, hasGAAPROIC := averageROIC(opIncomeMetric, equityMetric, debtMetric, cashMetric, taxMetric, pretaxMetric, 1)
	if !hasGAAPROIC && len(summary.Periods) > 1 {
		gaapROIC, hasGAAPROIC = averageROIC(opIncomeMetric, equityMetric, debtMetric, cashMetric, taxMetric, pretaxMetric, min(3, len(summary.Periods)-1))
	}

	// Intangible-Adjusted metrics
	latestIntCap := intCapMetric[0].Value
	latestAdjEBIT := adjEBITMetric[0].Value
	latestAdjEBITMargin := latestAdjEBIT / gaapRevenue

	adjInvestedCapMetric := fundamentals.NamedMetric(summary, "Adjusted Invested Capital")
	latestAdjInvestedCap := 0.0
	if len(adjInvestedCapMetric) > 0 && adjInvestedCapMetric[0].Present {
		latestAdjInvestedCap = adjInvestedCapMetric[0].Value
	}

	latestAdjROIC := 0.0
	hasAdjROIC := false
	if len(adjROICMetric) > 0 && adjROICMetric[0].Present {
		latestAdjROIC = adjROICMetric[0].Value
		hasAdjROIC = true
	}

	var validAdjROIC []float64
	for i := 0; i < len(adjROICMetric) && i < filter.Years; i++ {
		if adjROICMetric[i].Present && finite(adjROICMetric[i].Value) {
			validAdjROIC = append(validAdjROIC, adjROICMetric[i].Value)
		}
	}
	avgAdjROIC := latestAdjROIC
	if len(validAdjROIC) > 0 {
		avgAdjROIC = mean(validAdjROIC)
	}

	roicDelta := 0.0
	if hasAdjROIC && hasGAAPROIC {
		roicDelta = latestAdjROIC - gaapROIC
	}

	surging := false
	if hasAdjROIC {
		if len(adjROICMetric) > 1 && adjROICMetric[1].Present && latestAdjROIC > adjROICMetric[1].Value {
			surging = true
		} else if latestAdjROIC >= avgAdjROIC && latestAdjROIC >= 0.15 {
			surging = true
		} else if roicDelta >= 0.05 {
			surging = true
		}
	}

	// Market Valuation
	marketCap := ResolveMarketCap(s, ctx, summary, summary.Ticker, marketCaps, prices)
	hasMarketCap := marketCap > 0
	gaapPE := 0.0
	hasGAAP_PE := false
	if hasMarketCap && gaapNetIncome > 0 {
		gaapPE = marketCap / gaapNetIncome
		hasGAAP_PE = true
	}

	// Owner Earnings & Adjusted Owner Earnings
	ownerMetric := fundamentals.NamedMetric(summary, "SBC-Adjusted FCF Proxy")
	fcfMetric := fundamentals.NamedMetric(summary, "Free Cash Flow")
	rdMetric := fundamentals.NamedMetric(summary, "Research & Development")
	sgaMetric := fundamentals.NamedMetric(summary, "Selling, General & Administrative")

	ownerBase := 0.0
	hasOwner := false
	if len(ownerMetric) > 0 && ownerMetric[0].Present {
		ownerBase = ownerMetric[0].Value
		hasOwner = true
	} else if len(fcfMetric) > 0 && fcfMetric[0].Present {
		ownerBase = fcfMetric[0].Value
		hasOwner = true
	}

	currentRD := 0.0
	if len(rdMetric) > 0 && rdMetric[0].Present {
		currentRD = math.Abs(rdMetric[0].Value)
	}
	currentSGA := 0.0
	if len(sgaMetric) > 0 && sgaMetric[0].Present {
		currentSGA = math.Abs(sgaMetric[0].Value)
	}
	currentAmort := 0.0
	if len(amortMetric) > 0 && amortMetric[0].Present {
		currentAmort = amortMetric[0].Value
	}

	growthIntangible := math.Max(0, (currentRD+0.30*currentSGA)-currentAmort)
	adjustedOwnerEarnings := ownerBase + growthIntangible

	ownerEarningsYield := 0.0
	hasYield := false
	if hasMarketCap && hasOwner {
		ownerEarningsYield = adjustedOwnerEarnings / marketCap
		hasYield = true
	}

	// Filter Gates
	if filter.MinAdjustedROIC > 0 && (!hasAdjROIC || latestAdjROIC < filter.MinAdjustedROIC) {
		return zero, false
	}
	if filter.MinOwnerYield > 0 && (!hasYield || ownerEarningsYield < filter.MinOwnerYield) {
		return zero, false
	}
	if filter.RequireSurging && !surging {
		return zero, false
	}
	if filter.MaxGAAPROIC > 0 && hasGAAPROIC && gaapROIC > filter.MaxGAAPROIC {
		return zero, false
	}
	if filter.RequireGAAPExpensive {
		isExpensive := gaapUnprofitable || (hasGAAP_PE && gaapPE >= 25.0) || (hasGAAPROIC && gaapROIC < 0.15) || (roicDelta >= 0.05)
		if !isExpensive {
			return zero, false
		}
	}

	// Arbitrage Signal Classification
	signal := "Capitalized Intangible Moat"
	switch {
	case gaapNetIncome <= 0 && latestAdjEBIT > 0:
		signal = "GAAP Loss → Economic Profit"
	case hasGAAP_PE && gaapPE >= 35.0 && latestAdjROIC >= 0.20:
		signal = "Severe GAAP P/E Distortion"
	case hasGAAPROIC && roicDelta >= 0.10:
		signal = "Surging ROIC Arbitrage"
	case surging && latestAdjROIC >= 0.20:
		signal = "Surging ROIC Compounder"
	}

	score := (latestAdjROIC * 100 * 0.40) + (math.Max(0, roicDelta) * 100 * 0.20)
	if hasYield {
		score += (math.Max(0, ownerEarningsYield) * 100 * 0.40 * 5)
	} else {
		score += (latestAdjROIC * 100 * 0.40)
	}

	return ModernGAAPCandidate{
		Ticker:                company.Ticker,
		Company:               company.Name,
		SIC:                   company.SIC,
		SICDescription:        company.SICDescription,
		Currency:              currency,
		MarketCap:             marketCap,
		HasMarketCap:          hasMarketCap,
		GAAPRevenue:           gaapRevenue,
		GAAPNetIncome:         gaapNetIncome,
		GAAPOperatingIncome:   gaapOpIncome,
		GAAPOperatingMargin:   gaapOpMargin,
		GAAPROIC:              gaapROIC,
		HasGAAPROIC:           hasGAAPROIC,
		GAAP_PE:               gaapPE,
		HasGAAP_PE:            hasGAAP_PE,
		GAAPUnprofitable:      gaapUnprofitable,
		IntangibleCapital:     latestIntCap,
		AdjustedEBIT:          latestAdjEBIT,
		AdjustedEBITMargin:    latestAdjEBITMargin,
		AdjustedInvestedCap:   latestAdjInvestedCap,
		AdjustedROIC:          latestAdjROIC,
		HasAdjustedROIC:       hasAdjROIC,
		AvgAdjustedROIC:       avgAdjROIC,
		ROICDelta:             roicDelta,
		SurgingROIC:           surging,
		OwnerEarnings:         ownerBase,
		AdjustedOwnerEarnings: adjustedOwnerEarnings,
		OwnerEarningsYield:    ownerEarningsYield,
		HasOwnerEarningsYield: hasYield,
		ArbitrageScore:        score,
		ArbitrageSignal:       signal,
	}, true
}

// WriteModernGAAPTable prints the screening candidates in a formatted ASCII table.
func WriteModernGAAPTable(output io.Writer, candidates []ModernGAAPCandidate) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "RANK\tTICKER\tCOMPANY\tMARKET CAP\tGAAP P/E\tGAAP ROIC\tADJ ROIC\tROIC Δ\tINTANG CAP\tADJ EBIT\tOWNER YLD\tARBITRAGE SIGNAL"); err != nil {
		return err
	}
	for i, c := range candidates {
		mcapCell := "—"
		if c.HasMarketCap && c.MarketCap > 0 {
			mcapCell = formatSummaryValue(c.MarketCap, c.Currency)
		}
		peCell := "—"
		if c.GAAPUnprofitable {
			peCell = "Loss"
		} else if c.HasGAAP_PE && c.GAAP_PE > 0 {
			if c.GAAP_PE > 999 {
				peCell = ">999x"
			} else {
				peCell = fmt.Sprintf("%.1fx", c.GAAP_PE)
			}
		}
		gaapROICCell := formatPercent(c.GAAPROIC, c.HasGAAPROIC)
		adjROICCell := formatPercent(c.AdjustedROIC, c.HasAdjustedROIC)
		deltaCell := "—"
		if c.HasAdjustedROIC && c.HasGAAPROIC {
			deltaCell = fmt.Sprintf("%+.1f%%", c.ROICDelta*100)
		}
		intCapCell := formatSummaryValue(c.IntangibleCapital, c.Currency)
		adjEBITCell := formatSummaryValue(c.AdjustedEBIT, c.Currency)
		yieldCell := formatPercent(c.OwnerEarningsYield, c.HasOwnerEarningsYield)

		companyName := c.Company
		if len(companyName) > 24 {
			companyName = strings.TrimSpace(companyName[:23]) + "…"
		}

		if _, err := fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1, c.Ticker, companyName, mcapCell, peCell, gaapROICCell, adjROICCell, deltaCell, intCapCell, adjEBITCell, yieldCell, c.ArbitrageSignal); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(output, "\nModern GAAP Arbitrage (Peters & Taylor / Damodaran): R&D capitalized (5Y linear), 30% SG&A capitalized (3Y linear). ROIC Δ = Intangible-Adjusted ROIC − Standard GAAP ROIC. Owner Yield = Intangible-Adjusted Owner Earnings / Market Cap.")
	return err
}
