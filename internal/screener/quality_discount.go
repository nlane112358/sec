package screener

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"text/tabwriter"

	"sec/internal/fundamentals"
	"sec/internal/market"
	"sec/internal/store"
	"sec/internal/valuation"
)

type QualityDiscountCandidate struct {
	Ticker            string  `json:"ticker"`
	Company           string  `json:"company"`
	Sector            string  `json:"sector"`
	PeerGroup         string  `json:"peer_group"`
	Currency          string  `json:"currency"`
	QualityScore      float64 `json:"quality_score"`
	ROIC              float64 `json:"roic"`
	HasROIC           bool    `json:"has_roic"`
	AdjROIC           float64 `json:"adj_roic"`
	HasAdjROIC        bool    `json:"has_adj_roic"`
	OperatingMargin   float64 `json:"operating_margin"`
	FCFMargin         float64 `json:"fcf_margin"`
	ShareReduction    float64 `json:"share_reduction"`
	HasShareReduction bool    `json:"has_share_reduction"`
	Price             float64 `json:"price"`
	MarketCap         float64 `json:"market_cap"`
	DCFValue          float64 `json:"dcf_value"`
	Discount          float64 `json:"discount"` // Margin of safety: (DCFValue - Price) / DCFValue
	Upside            float64 `json:"upside"`   // Upside: (DCFValue - Price) / Price
	FCF               float64 `json:"fcf"`
	FCFYield          float64 `json:"fcf_yield"` // FCF / MarketCap
	EV_FCF            float64 `json:"ev_fcf"`
	EnterpriseValue   float64 `json:"enterprise_value"`
	NetDebt           float64 `json:"net_debt"`
	Cash              float64 `json:"cash"`
	Debt              float64 `json:"debt"`
	ImpliedGrowth     float64 `json:"implied_growth"`
	HistoricalGrowth  float64 `json:"historical_growth"`
	HasImpliedGrowth  bool    `json:"has_implied_growth"`
	AlphaSpread       float64 `json:"alpha_spread"` // HistoricalGrowth - ImpliedGrowth
	QAADScore         float64 `json:"qaad_score"`   // Composite Quality-at-a-Discount Score (0 - 100)
}

type QualityDiscountFilter struct {
	MinScore            float64
	MinROIC             float64
	MinDiscount         float64 // Minimum margin of safety: e.g. 0.15 for 15%
	MinFCFYield         float64 // Minimum FCF yield: e.g. 0.04 for 4%
	RequireBuybacks     bool
	MinOperatingConsist float64
	IncludeFinancials   bool
	SortBy              string // "qaad", "discount", "quality", "yield", "upside"
	DCFConfig           valuation.DCFConfig
}

// ScreenQualityDiscount identifies companies that pass strict quality hurdles
// and trade at a substantial discount (margin of safety) to DCF intrinsic value.
func ScreenQualityDiscount(
	ctx context.Context,
	s *store.Store,
	ranked []RankedCompany,
	marketCaps, prices map[string]float64,
	filter QualityDiscountFilter,
) ([]QualityDiscountCandidate, error) {
	candidates := make([]QualityDiscountCandidate, 0)

	for _, company := range ranked {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !filter.IncludeFinancials && company.Sector == "Financials" {
			continue
		}
		if filter.MinScore > 0 && company.Score < filter.MinScore {
			continue
		}
		if filter.MinROIC > 0 && (!company.HasROIC || company.ROIC < filter.MinROIC) {
			continue
		}
		if filter.MinOperatingConsist > 0 && (!company.HasOperatingMargin || company.OperatingConsistency < filter.MinOperatingConsist) {
			continue
		}
		if filter.RequireBuybacks && (!company.HasShareTrend || company.ShareReduction < 0) {
			continue
		}

		summary, err := fundamentals.LoadFinancialSummary(ctx, s, 6, company.Ticker)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if len(summary.Periods) == 0 {
			continue
		}

		marketCap := ResolveMarketCap(s, ctx, summary, company.Ticker, marketCaps, prices)
		if !finite(marketCap) || marketCap <= 0 {
			continue
		}

		// Resolve price per share
		price := valuationForTicker(prices, company.Ticker)
		sharesVal := fundamentals.NamedMetric(summary, "Shares Outstanding")
		var shares float64
		if len(sharesVal) > 0 && sharesVal[0].Present && sharesVal[0].Value > 0 {
			shares = sharesVal[0].Value
		} else if sShares, ok := s.LatestShareCount(ctx, company.Ticker); ok && sShares > 0 {
			shares = sShares
		}
		if price <= 0 && marketCap > 0 && shares > 0 {
			adrShares := market.OrdinaryToADRShares(company.Ticker, shares)
			if adrShares > 0 {
				price = marketCap / adrShares
			}
		}
		if !finite(price) || price <= 0 {
			continue
		}

		// Forward DCF Configuration
		dcfCfg := filter.DCFConfig
		if dcfCfg.WACC == nil {
			dcfCfg.WACC = &valuation.WACCConfig{
				RiskFreeRate: 0.0484, EquityRiskPremium: 0.055, Beta: 1.0, CostOfDebt: 0.05, TaxRate: 0.21,
				UseEffectiveTaxRate: true, UseEstimatedCostDebt: true,
			}
		}
		waccCopy := *dcfCfg.WACC
		dcfCfg.WACC = &waccCopy
		dcfCfg.WACC.MarketCap = marketCap
		if dcfCfg.Mode == "" {
			dcfCfg.Mode = valuation.DCFModeFCF
		}
		if dcfCfg.Years <= 0 {
			dcfCfg.Years = 5
		}
		if dcfCfg.TerminalRate <= 0 {
			dcfCfg.TerminalRate = 0.025
		}

		dcfResult, err := valuation.CalculateDCF(summary, dcfCfg)
		if err != nil {
			// Fallback: If 3-year historical FCF CAGR is unavailable, anchor forward growth to revenue CAGR
			if dcfCfg.GrowthRate == 0 {
				fallbackCfg := dcfCfg
				growth := company.RevenueCAGR
				if !finite(growth) {
					growth = 0.05
				}
				fallbackCfg.GrowthRate = math.Max(-0.10, math.Min(0.20, growth))
				dcfResult, err = valuation.CalculateDCF(summary, fallbackCfg)
				if err != nil {
					// Second fallback: try revenue mode
					fallbackCfg.Mode = valuation.DCFModeRevenue
					dcfResult, err = valuation.CalculateDCF(summary, fallbackCfg)
				}
			}
		}
		if err != nil || !finite(dcfResult.ValuePerShare) || dcfResult.ValuePerShare <= 0 {
			continue
		}

		dcfValue := dcfResult.ValuePerShare
		discount := (dcfValue - price) / dcfValue
		upside := (dcfValue - price) / price

		if filter.MinDiscount > -1 && discount < filter.MinDiscount {
			continue
		}

		// Balance sheet details
		cashVal := fundamentals.NamedMetric(summary, "Cash & Equivalents")
		debtVal := fundamentals.NamedMetric(summary, "Debt / Borrowings")
		cash := 0.0
		debt := 0.0
		if len(cashVal) > 0 && cashVal[0].Present {
			cash = cashVal[0].Value
		}
		if len(debtVal) > 0 && debtVal[0].Present {
			debt = debtVal[0].Value
		}
		netDebt := debt - cash
		marketEV := math.Max(0, marketCap+debt-cash)

		// Latest FCF & FCF Yield
		fcfVal := fundamentals.NamedMetric(summary, "Free Cash Flow")
		var latestFCF float64
		if len(fcfVal) > 0 && fcfVal[0].Present {
			latestFCF = fcfVal[0].Value
		}
		fcfYield := 0.0
		if marketCap > 0 {
			fcfYield = latestFCF / marketCap
		}
		if filter.MinFCFYield > 0 && fcfYield < filter.MinFCFYield {
			continue
		}

		evFCF := 0.0
		if latestFCF > 0 {
			evFCF = marketEV / latestFCF
		}

		// Reverse DCF for Expectations Arbitrage
		revCfg := dcfCfg
		revCfg.WACC = dcfCfg.WACC
		reverse, revErr := valuation.SolveReverseDCF(summary, revCfg)
		impliedGrowth := 0.0
		historicalGrowth := 0.0
		alphaSpread := 0.0
		hasImplied := false
		if revErr == nil {
			hasImplied = true
			impliedGrowth = reverse.ImpliedGrowth
			if reverse.HasHistoricalCAGR {
				historicalGrowth = reverse.HistoricalCAGR
			} else if finite(company.RevenueCAGR) {
				historicalGrowth = company.RevenueCAGR
			}
			alphaSpread = historicalGrowth - impliedGrowth
		}

		// Intangible-Adjusted ROIC
		adjROICVal := fundamentals.NamedMetric(summary, "Intangible-Adjusted ROIC")
		adjROIC := 0.0
		hasAdjROIC := false
		if len(adjROICVal) > 0 && adjROICVal[0].Present && finite(adjROICVal[0].Value) {
			adjROIC = adjROICVal[0].Value
			hasAdjROIC = true
		}

		// Calculate composite QAAD Score (0 - 100)
		// 1. Quality Component (50%): Company Score (0-100)
		qScore := company.Score

		// 2. Discount Component (50%):
		// - Margin of Safety (DCF Discount): 50% of discount component
		//   discount of 50%+ maps to 100; discount of -20% maps to 0
		discountScore := math.Max(0, math.Min(100, (discount+0.20)/0.70*100))

		// - FCF Yield: 30% of discount component
		//   yield of 10%+ maps to 100; yield <= 0 maps to 0
		yieldScore := math.Max(0, math.Min(100, (fcfYield/0.10)*100))

		// - Alpha Spread: 20% of discount component
		//   spread of +15%+ maps to 100; spread <= -10% maps to 0
		spreadScore := 50.0
		if hasImplied {
			spreadScore = math.Max(0, math.Min(100, (alphaSpread+0.10)/0.25*100))
		}

		dScore := 0.50*discountScore + 0.30*yieldScore + 0.20*spreadScore
		qaadScore := 0.50*qScore + 0.50*dScore

		candidates = append(candidates, QualityDiscountCandidate{
			Ticker:            company.Ticker,
			Company:           company.Company,
			Sector:            company.Sector,
			PeerGroup:         company.PeerGroup,
			Currency:          company.Currency,
			QualityScore:      company.Score,
			ROIC:              company.ROIC,
			HasROIC:           company.HasROIC,
			AdjROIC:           adjROIC,
			HasAdjROIC:        hasAdjROIC,
			OperatingMargin:   company.OperatingMargin,
			FCFMargin:         company.FCFMargin,
			ShareReduction:    company.ShareReduction,
			HasShareReduction: company.HasShareTrend,
			Price:             price,
			MarketCap:         marketCap,
			DCFValue:          dcfValue,
			Discount:          discount,
			Upside:            upside,
			FCF:               latestFCF,
			FCFYield:          fcfYield,
			EV_FCF:            evFCF,
			EnterpriseValue:   dcfResult.EnterpriseValue,
			NetDebt:           netDebt,
			Cash:              cash,
			Debt:              debt,
			ImpliedGrowth:     impliedGrowth,
			HistoricalGrowth:  historicalGrowth,
			HasImpliedGrowth:  hasImplied,
			AlphaSpread:       alphaSpread,
			QAADScore:         qaadScore,
		})
	}

	// Sort candidates
	switch strings.ToLower(strings.TrimSpace(filter.SortBy)) {
	case "discount", "mos", "margin-of-safety":
		sort.Slice(candidates, func(i, j int) bool {
			if math.Abs(candidates[i].Discount-candidates[j].Discount) > 1e-9 {
				return candidates[i].Discount > candidates[j].Discount
			}
			return candidates[i].QAADScore > candidates[j].QAADScore
		})
	case "quality", "score":
		sort.Slice(candidates, func(i, j int) bool {
			if math.Abs(candidates[i].QualityScore-candidates[j].QualityScore) > 1e-9 {
				return candidates[i].QualityScore > candidates[j].QualityScore
			}
			return candidates[i].Discount > candidates[j].Discount
		})
	case "yield", "fcf-yield":
		sort.Slice(candidates, func(i, j int) bool {
			if math.Abs(candidates[i].FCFYield-candidates[j].FCFYield) > 1e-9 {
				return candidates[i].FCFYield > candidates[j].FCFYield
			}
			return candidates[i].QAADScore > candidates[j].QAADScore
		})
	case "upside":
		sort.Slice(candidates, func(i, j int) bool {
			if math.Abs(candidates[i].Upside-candidates[j].Upside) > 1e-9 {
				return candidates[i].Upside > candidates[j].Upside
			}
			return candidates[i].QAADScore > candidates[j].QAADScore
		})
	default: // "qaad" / composite
		sort.Slice(candidates, func(i, j int) bool {
			if math.Abs(candidates[i].QAADScore-candidates[j].QAADScore) > 1e-9 {
				return candidates[i].QAADScore > candidates[j].QAADScore
			}
			return candidates[i].Discount > candidates[j].Discount
		})
	}

	return candidates, nil
}

// WriteQualityDiscountTable outputs a formatted table of Quality-at-a-Discount candidates.
func WriteQualityDiscountTable(output io.Writer, candidates []QualityDiscountCandidate) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "RANK\tTICKER\tCOMPANY\tQAAD SCORE\tQUAL SCORE\tROIC\tFCF YLD\tPRICE\tDCF VALUE\tDISCOUNT\tUPSIDE\tEV/FCF\tIMPLIED g\tALPHA SPREAD"); err != nil {
		return err
	}

	for idx, c := range candidates {
		roicStr := "n/a"
		if c.HasROIC {
			roicStr = fmt.Sprintf("%.1f%%", c.ROIC*100)
		}
		if c.HasAdjROIC {
			roicStr = fmt.Sprintf("%.1f%%*", c.AdjROIC*100)
		}

		impliedStr := "n/a"
		alphaStr := "n/a"
		if c.HasImpliedGrowth {
			impliedStr = fmt.Sprintf("%.1f%%", c.ImpliedGrowth*100)
			if c.AlphaSpread > 0 {
				alphaStr = fmt.Sprintf("+%.1f%%", c.AlphaSpread*100)
			} else {
				alphaStr = fmt.Sprintf("%.1f%%", c.AlphaSpread*100)
			}
		}

		evFCFStr := "n/a"
		if c.EV_FCF > 0 {
			evFCFStr = fmt.Sprintf("%.1fx", c.EV_FCF)
		}

		discountStr := fmt.Sprintf("%.1f%%", c.Discount*100)
		if c.Discount > 0 {
			discountStr = fmt.Sprintf("+%.1f%%", c.Discount*100)
		}

		upsideStr := fmt.Sprintf("%.1f%%", c.Upside*100)
		if c.Upside > 0 {
			upsideStr = fmt.Sprintf("+%.1f%%", c.Upside*100)
		}

		companyName := c.Company
		if len(companyName) > 24 {
			companyName = strings.TrimSpace(companyName[:23]) + "…"
		}

		if _, err := fmt.Fprintf(writer, "%d\t%s\t%s\t%.1f\t%.1f\t%s\t%.1f%%\t$%.2f\t$%.2f\t%s\t%s\t%s\t%s\t%s\n",
			idx+1,
			c.Ticker,
			companyName,
			c.QAADScore,
			c.QualityScore,
			roicStr,
			c.FCFYield*100,
			c.Price,
			c.DCFValue,
			discountStr,
			upsideStr,
			evFCFStr,
			impliedStr,
			alphaStr,
		); err != nil {
			return err
		}
	}

	if err := writer.Flush(); err != nil {
		return err
	}

	_, err := fmt.Fprintln(output, "\n* Asterisk indicates Peters & Taylor Intangible-Adjusted ROIC. Discount = Margin of Safety ((DCF - Price) / DCF). QAAD Score balances 50% Quality + 50% Discount/Yield.")
	return err
}
