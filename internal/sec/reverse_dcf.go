package sec

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

const DCFModeOwnerEarnings DCFMode = "owner"

type ReverseDCFResult struct {
	Ticker                string
	Company               string
	Mode                  DCFMode
	Unit                  string
	MarketCap             float64
	Debt                  float64
	Cash                  float64
	EnterpriseValue       float64
	BaseCashFlow          float64
	BaseRevenue           float64
	BaseOwnerMargin       float64
	OwnerEarningsAdjusted bool
	WACC                  float64
	DiscountRate          float64
	CostOfEquity          float64
	AfterTaxCostOfDebt    float64
	EffectiveTaxRate      float64
	TerminalRate          float64
	ForecastYears         int
	ImpliedGrowth         float64
	HistoricalCAGR        float64
	HasHistoricalCAGR     bool
	AlphaSpread           float64 // HistoricalCAGR - ImpliedGrowth
	IntrinsicAtHistCAGR   float64
	MarginOfSafety        float64
}

type ArbitrageCandidate struct {
	Ranked                 RankedCompany
	Reverse                ReverseDCFResult
	HistoricalGrowth       float64
	HistoricalGrowthSource string
	HasHistoricalGrowth    bool
	PeerMedianGrowth       float64
	HasPeerMedianGrowth    bool
	ConservativeAnchor     float64
	AlphaSpreadHist        float64
	AlphaSpreadPeer        float64
	ConservativeSpread     float64
	ExpectationsRatio      float64
}

type ArbitrageFilter struct {
	MinScore            float64
	MinROIC             float64
	MinOperatingConsist float64
	RequireBuybacks     bool
	MaxImpliedGrowth    float64
	DCFConfig           DCFConfig
}

// SolveReverseDCF inverts the selected DCF model to find the annual growth rate
// that makes its enterprise value equal to market capitalization plus debt less cash.
func SolveReverseDCF(summary FinancialSummary, config DCFConfig) (ReverseDCFResult, error) {
	var result ReverseDCFResult
	if len(summary.Periods) == 0 {
		return result, fmt.Errorf("no annual periods available for %s", summary.Ticker)
	}
	if config.WACC == nil || !finite(config.WACC.MarketCap) || config.WACC.MarketCap <= 0 {
		return result, fmt.Errorf("reverse DCF requires a positive --market-cap input")
	}
	if config.Mode == "" {
		config.Mode = DCFModeOwnerEarnings
	}
	if config.Mode != DCFModeOwnerEarnings && config.Mode != DCFModeFCF && config.Mode != DCFModeRevenue {
		return result, fmt.Errorf("reverse DCF mode must be owner, fcf, or revenue")
	}
	years := config.Years
	if years == 0 {
		years = 5
	}
	if years < 1 || years > 20 {
		return result, fmt.Errorf("forecast years must be between 1 and 20")
	}
	terminal := config.TerminalRate
	if terminal == 0 {
		terminal = 0.025
	}
	if !finite(terminal) || terminal < 0 {
		return result, fmt.Errorf("terminal growth must be finite and non-negative")
	}

	waccConfig := *config.WACC
	if waccConfig.DebtOverride == nil {
		waccConfig.DebtOverride = config.DebtOverride
	}
	waccResult, err := CalculateWACC(summary, waccConfig)
	if err != nil {
		return result, fmt.Errorf("calculate WACC: %w", err)
	}
	discountRate := waccResult.Rate
	if config.DiscountRate != 0 {
		discountRate = config.DiscountRate
	}
	if !finite(discountRate) || discountRate <= 0 || discountRate >= 1 {
		return result, fmt.Errorf("discount rate must be greater than 0%% and less than 100%%")
	}
	if terminal >= discountRate {
		return result, fmt.Errorf("terminal growth must be below the discount rate")
	}

	cashValues := metricValues(summary, "Cash & Equivalents")
	cash := 0.0
	if len(cashValues) > 0 && cashValues[0].Present {
		if !finite(cashValues[0].Value) || cashValues[0].Value < 0 {
			return result, fmt.Errorf("latest cash and equivalents must be finite and non-negative")
		}
		cash = cashValues[0].Value
	}
	debt := waccResult.GrossDebt
	marketCap := waccConfig.MarketCap
	enterpriseValue := marketCap + debt - cash
	if !finite(enterpriseValue) || enterpriseValue <= 0 {
		return result, fmt.Errorf("%s has negative or zero enterprise value (market cap %.2f, debt %.2f, cash %.2f)", summary.Ticker, marketCap, debt, cash)
	}

	baseCashFlow, unit, historicalValues, err := reverseBaseCashFlow(summary, config, waccResult.TaxRate, discountRate, years)
	if err != nil {
		return result, err
	}
	ownerEarningsAdjusted := false
	if config.Mode == DCFModeOwnerEarnings {
		ownerValues := metricValues(summary, "SBC-Adjusted FCF Proxy")
		ownerEarningsAdjusted = len(ownerValues) > 0 && ownerValues[0].Present
		if config.NormalizeCapEx {
			sbcValues := metricValues(summary, "Stock-Based Compensation")
			ownerEarningsAdjusted = len(sbcValues) > 0 && sbcValues[0].Present
		}
	}
	if unit != "" && waccResult.Currency != "" && !strings.EqualFold(unit, waccResult.Currency) {
		return result, fmt.Errorf("DCF cash flows, debt, and cash must use the same currency unit")
	}
	baseRevenue := 0.0
	revenueValues := metricValues(summary, "Revenue")
	if len(revenueValues) > 0 && revenueValues[0].Present {
		baseRevenue = revenueValues[0].Value
	}
	historicalGrowth, hasHistoricalGrowth := metricCAGR(historicalValues, summary.Periods)

	valueAtGrowth := func(growth float64) (float64, error) {
		return reverseEnterpriseValue(summary, config, growth, discountRate, waccResult.TaxRate, terminal, years, baseCashFlow)
	}
	const lowerGrowth, upperGrowth = -0.75, 2.50
	lowerValue, err := valueAtGrowth(lowerGrowth)
	if err != nil {
		return result, fmt.Errorf("value reverse DCF at %.0f%% growth: %w", lowerGrowth*100, err)
	}
	upperValue, err := valueAtGrowth(upperGrowth)
	if err != nil {
		return result, fmt.Errorf("value reverse DCF at %.0f%% growth: %w", upperGrowth*100, err)
	}
	if lowerValue > upperValue {
		return result, fmt.Errorf("selected DCF model is not increasing over the supported growth range")
	}
	if enterpriseValue < lowerValue || enterpriseValue > upperValue {
		return result, fmt.Errorf("enterprise value %.2f is outside the model range [%.2f, %.2f] for growth from -75%% to 250%%", enterpriseValue, lowerValue, upperValue)
	}
	low, high := lowerGrowth, upperGrowth
	for iteration := 0; iteration < 50; iteration++ {
		mid := (low + high) / 2
		value, err := valueAtGrowth(mid)
		if err != nil {
			return result, fmt.Errorf("value reverse DCF at %.4f growth: %w", mid, err)
		}
		if value < enterpriseValue {
			low = mid
		} else {
			high = mid
		}
	}
	impliedGrowth := (low + high) / 2

	result = ReverseDCFResult{
		Ticker: summary.Ticker, Company: summary.Company, Mode: config.Mode, Unit: unit,
		MarketCap: marketCap, Debt: debt, Cash: cash, EnterpriseValue: enterpriseValue,
		BaseCashFlow: baseCashFlow, BaseRevenue: baseRevenue, WACC: waccResult.Rate,
		OwnerEarningsAdjusted: ownerEarningsAdjusted,
		CostOfEquity:          waccResult.CostOfEquity, DiscountRate: discountRate,
		AfterTaxCostOfDebt: waccResult.CostOfDebt * (1 - waccResult.TaxRate),
		EffectiveTaxRate:   waccResult.TaxRate, TerminalRate: terminal, ForecastYears: years,
		ImpliedGrowth: impliedGrowth, HistoricalCAGR: historicalGrowth,
		HasHistoricalCAGR: hasHistoricalGrowth,
	}
	if config.Mode == DCFModeOwnerEarnings && baseRevenue > 0 && strings.EqualFold(unit, latestUnit(revenueValues)) {
		result.BaseOwnerMargin = baseCashFlow / baseRevenue
	}
	if hasHistoricalGrowth {
		cappedHistorical := math.Min(math.Max(historicalGrowth, -0.20), 0.35)
		enterpriseAtHistorical, err := valueAtGrowth(cappedHistorical)
		if err != nil {
			return ReverseDCFResult{}, fmt.Errorf("value reverse DCF at historical growth: %w", err)
		}
		result.IntrinsicAtHistCAGR = math.Max(0, enterpriseAtHistorical-debt+cash)
		if marketCap > 0 {
			result.MarginOfSafety = (result.IntrinsicAtHistCAGR - marketCap) / marketCap
		}
		result.AlphaSpread = historicalGrowth - impliedGrowth
	}
	return result, nil
}

func reverseBaseCashFlow(summary FinancialSummary, config DCFConfig, taxRate, discountRate float64, years int) (float64, string, []SummaryValue, error) {
	switch config.Mode {
	case DCFModeFCF:
		values := historicalFCFValues(summary, config.NormalizeCapEx)
		base, _, err := fcfForecast(summary, 0, config.NormalizeCapEx, 1, 0)
		if err != nil {
			return 0, "", nil, err
		}
		unit := ""
		if len(values) > 0 {
			unit = values[0].Unit
		}
		return base, unit, values, nil
	case DCFModeRevenue:
		revenue := metricValues(summary, "Revenue")
		if len(revenue) == 0 || !revenue[0].Present || revenue[0].Value <= 0 {
			return 0, "", nil, fmt.Errorf("latest annual revenue is missing or non-positive")
		}
		if !strings.EqualFold(revenue[0].Unit, latestUnit(metricValues(summary, "Operating Income"))) {
			return 0, "", nil, fmt.Errorf("revenue and operating income must use the same currency unit")
		}
		_, _, _, _, base, _, err := revenueDrivenForecast(summary, 0, taxRate, config.NormalizeCapEx, years, discountRate)
		if err != nil {
			return 0, "", nil, err
		}
		return base, revenue[0].Unit, revenue, nil
	case DCFModeOwnerEarnings:
		ownerValues := metricValues(summary, "SBC-Adjusted FCF Proxy")
		freeCashFlow := metricValues(summary, "Free Cash Flow")
		if config.NormalizeCapEx {
			opcf := metricValues(summary, "Operating Cash Flow")
			da := metricValues(summary, "Depreciation & Amortization")
			sbc := metricValues(summary, "Stock-Based Compensation")
			if len(opcf) == 0 || !opcf[0].Present || len(da) == 0 || !da[0].Present || !strings.EqualFold(opcf[0].Unit, da[0].Unit) {
				return 0, "", nil, fmt.Errorf("owner-earnings CapEx normalization requires latest operating cash flow and depreciation and amortization")
			}
			base := opcf[0].Value - math.Abs(da[0].Value)
			if len(sbc) > 0 && sbc[0].Present {
				if !strings.EqualFold(opcf[0].Unit, sbc[0].Unit) {
					return 0, "", nil, fmt.Errorf("operating cash flow and stock-based compensation must use the same currency unit")
				}
				base -= math.Abs(sbc[0].Value)
			}
			values := make([]SummaryValue, minInt(len(opcf), minInt(len(da), len(sbc))))
			for i := range values {
				if opcf[i].Present && da[i].Present && sbc[i].Present && strings.EqualFold(opcf[i].Unit, da[i].Unit) && strings.EqualFold(opcf[i].Unit, sbc[i].Unit) {
					values[i] = SummaryValue{Value: opcf[i].Value - math.Abs(da[i].Value) - math.Abs(sbc[i].Value), Unit: opcf[i].Unit, Present: true}
				}
			}
			if base <= 0 || !finite(base) {
				return 0, "", nil, fmt.Errorf("latest normalized owner earnings must be positive")
			}
			return base, opcf[0].Unit, values, nil
		}
		base, unit, values, err := latestPositiveMetric(ownerValues, freeCashFlow)
		if err != nil {
			return 0, "", nil, err
		}
		if base <= 0 {
			return 0, "", nil, fmt.Errorf("latest owner earnings are non-positive")
		}
		return base, unit, values, nil
	default:
		return 0, "", nil, fmt.Errorf("unsupported reverse DCF mode %q", config.Mode)
	}
}

func latestPositiveMetric(primary, fallback []SummaryValue) (float64, string, []SummaryValue, error) {
	if len(primary) > 0 && primary[0].Present {
		if !finite(primary[0].Value) || primary[0].Value <= 0 {
			return 0, "", nil, fmt.Errorf("latest SBC-adjusted owner earnings are non-positive")
		}
		return primary[0].Value, primary[0].Unit, primary, nil
	}
	if len(fallback) > 0 && fallback[0].Present {
		if !finite(fallback[0].Value) || fallback[0].Value <= 0 {
			return 0, "", nil, fmt.Errorf("latest free cash flow is non-positive")
		}
		return fallback[0].Value, fallback[0].Unit, fallback, nil
	}
	return 0, "", nil, fmt.Errorf("no positive latest owner-earnings or free-cash-flow metric is available")
}

func reverseEnterpriseValue(summary FinancialSummary, config DCFConfig, growth, discountRate, taxRate, terminal float64, years int, ownerBase float64) (float64, error) {
	if !finite(growth) || growth <= -1 {
		return 0, fmt.Errorf("growth must be finite and greater than -100%%")
	}
	var projections []DCFProjection
	switch config.Mode {
	case DCFModeFCF:
		_, forecast, err := fcfForecast(summary, growth, config.NormalizeCapEx, years, discountRate)
		if err != nil {
			return 0, err
		}
		projections = forecast
	case DCFModeRevenue:
		var err error
		_, _, _, _, _, projections, err = revenueDrivenForecast(summary, growth, taxRate, config.NormalizeCapEx, years, discountRate)
		if err != nil {
			return 0, err
		}
	default:
		projections = make([]DCFProjection, 0, years)
		cashFlow := ownerBase
		for year := 1; year <= years; year++ {
			cashFlow *= 1 + growth
			projections = append(projections, DCFProjection{Year: year, FreeCashFlow: cashFlow})
		}
	}
	return enterpriseValueFromProjections(projections, discountRate, terminal, years)
}

func enterpriseValueFromProjections(projections []DCFProjection, discountRate, terminal float64, years int) (float64, error) {
	if len(projections) == 0 {
		return 0, fmt.Errorf("DCF forecast did not produce any cash flows")
	}
	pvExplicit := 0.0
	for _, projection := range projections {
		pvExplicit += projection.FreeCashFlow / math.Pow(1+discountRate, float64(projection.Year))
	}
	lastCashFlow := projections[len(projections)-1].FreeCashFlow
	terminalValue := lastCashFlow * (1 + terminal) / (discountRate - terminal)
	value := pvExplicit + terminalValue/math.Pow(1+discountRate, float64(years))
	if !finite(value) {
		return 0, fmt.Errorf("DCF valuation overflowed; reduce growth or forecast years")
	}
	return value, nil
}

func metricCAGR(values []SummaryValue, periods []SummaryPeriod) (float64, bool) {
	if len(values) < 2 || len(periods) < 2 || !values[0].Present || values[0].Value <= 0 {
		return 0, false
	}
	latestYear, latestYearErr := strconv.Atoi(periods[0].Year)
	if latestYearErr != nil {
		return 0, false
	}
	for index := len(values) - 1; index >= 1; index-- {
		if index >= len(periods) || !values[index].Present || values[index].Value <= 0 || !strings.EqualFold(values[0].Unit, values[index].Unit) {
			continue
		}
		oldestYear, err := strconv.Atoi(periods[index].Year)
		if err != nil || oldestYear >= latestYear {
			continue
		}
		years := float64(latestYear - oldestYear)
		cagr := math.Pow(values[0].Value/values[index].Value, 1/years) - 1
		if finite(cagr) && cagr > -1 {
			return cagr, true
		}
	}
	return 0, false
}

func latestUnit(values []SummaryValue) string {
	if len(values) > 0 && values[0].Present {
		return values[0].Unit
	}
	return ""
}

// ScreenExpectationsArbitrage joins quality-ranked companies to market prices
// and solves their mode-specific implied growth rates.
func (s *Store) ScreenExpectationsArbitrage(ctx context.Context, ranked []RankedCompany, marketCaps, prices map[string]float64, filter ArbitrageFilter) ([]ArbitrageCandidate, error) {
	if filter.DCFConfig.WACC == nil {
		return nil, fmt.Errorf("arbitrage screening requires WACC configuration")
	}
	peerGrowths := make(map[string][]float64)
	for _, company := range ranked {
		if company.Sector == "Financials" || company.PeerGroup == "" {
			continue
		}
		growth := company.RevenueCAGR
		if company.HasEPSGrowth && finite(company.EPSGrowth) {
			growth = (growth + company.EPSGrowth) / 2
		}
		if finite(growth) {
			peerGrowths[company.PeerGroup] = append(peerGrowths[company.PeerGroup], growth)
		}
	}
	peerMedians := make(map[string]float64, len(peerGrowths))
	for group, growths := range peerGrowths {
		sort.Float64s(growths)
		middle := len(growths) / 2
		if len(growths)%2 == 1 {
			peerMedians[group] = growths[middle]
		} else if len(growths) > 0 {
			peerMedians[group] = (growths[middle-1] + growths[middle]) / 2
		}
	}

	candidates := make([]ArbitrageCandidate, 0)
	for _, company := range ranked {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if company.Sector == "Financials" || company.Score < filter.MinScore {
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

		summary, err := s.FinancialSummary(ctx, 6, company.Ticker)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		marketCap := valuationForTicker(marketCaps, company.Ticker)
		if !finite(marketCap) || marketCap <= 0 {
			marketCap = 0
			price := valuationForTicker(prices, company.Ticker)
			if finite(price) && price > 0 {
				var shareCount float64
				for _, metricName := range []string{"Shares Outstanding", "Diluted Weighted Average Shares"} {
					values := metricValues(summary, metricName)
					for index := 0; index < len(summary.Periods) && index < len(values); index++ {
						value := values[index]
						if isShareCount(value) && finite(value.Value) && value.Value > 0 {
							shareCount = value.Value
							break
						}
					}
					if shareCount > 0 {
						break
					}
				}
				if shareCount <= 0 {
					if databaseShares, ok := s.LatestShareCount(ctx, company.Ticker); ok {
						shareCount = databaseShares
					}
				}
				if shareCount > 0 {
					marketCap = price * shareCount
				}
			}
		}
		if !finite(marketCap) || marketCap <= 0 {
			continue
		}

		config := filter.DCFConfig
		config.WACC = new(WACCConfig)
		*config.WACC = *filter.DCFConfig.WACC
		config.WACC.MarketCap = marketCap
		if config.WACC.DebtOverride == nil {
			config.WACC.DebtOverride = config.DebtOverride
		}
		if config.Mode == "" {
			config.Mode = DCFModeOwnerEarnings
		}
		reverse, err := SolveReverseDCF(summary, config)
		if err != nil || (filter.MaxImpliedGrowth > 0 && reverse.ImpliedGrowth > filter.MaxImpliedGrowth) {
			continue
		}
		historicalGrowth, historicalSource, hasHistorical := reverse.HistoricalCAGR, "mode-specific cash flow CAGR", reverse.HasHistoricalCAGR
		if !hasHistorical && finite(company.RevenueCAGR) {
			historicalGrowth, historicalSource, hasHistorical = company.RevenueCAGR, "ranked revenue CAGR", true
		}
		peerMedian, hasPeerMedian := peerMedians[company.PeerGroup]
		alphaHist, anchor, alphaPeer := 0.0, historicalGrowth, 0.0
		if hasHistorical {
			alphaHist = historicalGrowth - reverse.ImpliedGrowth
		}
		if hasPeerMedian {
			alphaPeer = peerMedian - reverse.ImpliedGrowth
			if hasHistorical {
				anchor = math.Min(historicalGrowth, peerMedian)
			} else {
				anchor = peerMedian
			}
		}
		expectationsRatio := 0.0
		if hasHistorical && historicalGrowth > 0.001 {
			expectationsRatio = reverse.ImpliedGrowth / historicalGrowth
		}
		candidates = append(candidates, ArbitrageCandidate{
			Ranked: company, Reverse: reverse, HistoricalGrowth: historicalGrowth,
			HistoricalGrowthSource: historicalSource, HasHistoricalGrowth: hasHistorical,
			PeerMedianGrowth: peerMedian, HasPeerMedianGrowth: hasPeerMedian,
			ConservativeAnchor: anchor, AlphaSpreadHist: alphaHist, AlphaSpreadPeer: alphaPeer,
			ConservativeSpread: anchor - reverse.ImpliedGrowth, ExpectationsRatio: expectationsRatio,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].HasHistoricalGrowth != candidates[j].HasHistoricalGrowth {
			return candidates[i].HasHistoricalGrowth
		}
		if math.Abs(candidates[i].AlphaSpreadHist-candidates[j].AlphaSpreadHist) > 1e-9 {
			return candidates[i].AlphaSpreadHist > candidates[j].AlphaSpreadHist
		}
		if candidates[i].Ranked.Score != candidates[j].Ranked.Score {
			return candidates[i].Ranked.Score > candidates[j].Ranked.Score
		}
		return candidates[i].Ranked.Ticker < candidates[j].Ranked.Ticker
	})
	return candidates, nil
}

// LatestShareCount returns the latest positive SEC share count across the saved
// instant and diluted weighted-average share facts for a company.
// When two share-count facts have the same (period_end, filing_date), it picks the
// larger value (ORDER BY ... value DESC). It also cross-checks that Shares Outstanding
// is at least 50% of Diluted Weighted Average Shares to avoid picking tiny Class B/C share
// dimensions from dual-class structures (e.g. ERIE).
func (s *Store) LatestShareCount(ctx context.Context, ticker string) (float64, bool) {
	cik, _, _, err := s.companyForTicker(ctx, ticker)
	if err != nil {
		return 0, false
	}
	var outstanding, diluted float64
	_ = s.db.QueryRowContext(ctx, `
		SELECT value FROM sec_facts
		WHERE cik = ?
		  AND concept IN (
		      'EntityCommonStockSharesOutstanding',
		      'CommonStockSharesOutstanding',
		      'NumberOfSharesOutstanding'
		  )
		  AND unit = 'shares'
		  AND value > 0
		ORDER BY period_end DESC, filing_date DESC, value DESC
		LIMIT 1`, cik).Scan(&outstanding)

	_ = s.db.QueryRowContext(ctx, `
		SELECT value FROM sec_facts
		WHERE cik = ?
		  AND concept IN (
		      'WeightedAverageNumberOfDilutedSharesOutstanding',
		      'WeightedAverageNumberOfSharesOutstandingDiluted',
		      'DilutedWeightedAverageNumberOfOrdinarySharesOutstanding'
		  )
		  AND unit = 'shares'
		  AND value > 0
		ORDER BY period_end DESC, filing_date DESC, value DESC
		LIMIT 1`, cik).Scan(&diluted)

	hasOutstanding := finite(outstanding) && outstanding > 0
	hasDiluted := finite(diluted) && diluted > 0

	if hasOutstanding && hasDiluted {
		if outstanding < 0.5*diluted {
			return diluted, true
		}
		return outstanding, true
	}
	if hasOutstanding {
		return outstanding, true
	}
	if hasDiluted {
		return diluted, true
	}
	return 0, false
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

func WriteReverseDCFResult(output io.Writer, result ReverseDCFResult) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintf(writer, "%s (%s) — Reverse DCF Implied Expectations (%s mode)\n", result.Company, result.Ticker, result.Mode); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Market Capitalization:\t%s\n", formatSummaryValue(result.MarketCap, result.Unit)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Enterprise Value:\t%s (Debt: %s, Cash: %s)\n", formatSummaryValue(result.EnterpriseValue, result.Unit), formatSummaryValue(result.Debt, result.Unit), formatSummaryValue(result.Cash, result.Unit)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Base Cash Flow (t=0):\t%s\n", formatSummaryValue(result.BaseCashFlow, result.Unit)); err != nil {
		return err
	}
	if result.Mode == DCFModeOwnerEarnings {
		marginLabel := "Owner Earnings Margin (FCF fallback)"
		if result.OwnerEarningsAdjusted {
			marginLabel = "SBC-Adjusted Owner Margin"
		}
		if _, err := fmt.Fprintf(writer, "%s:\t%.1f%%\n", marginLabel, result.BaseOwnerMargin*100); err != nil {
			return err
		}
	}
	if result.Mode == DCFModeRevenue && result.BaseRevenue > 0 {
		if _, err := fmt.Fprintf(writer, "Base Revenue (t=0):\t%s\n", formatSummaryValue(result.BaseRevenue, result.Unit)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "Discount Rate:\t%.2f%% (calculated WACC: %.2f%%; Ke: %.2f%%, Kd after-tax: %.2f%%, Effective tax: %.1f%%)\n", result.DiscountRate*100, result.WACC*100, result.CostOfEquity*100, result.AfterTaxCostOfDebt*100, result.EffectiveTaxRate*100); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Terminal Growth Rate:\t%.2f%% (after %d explicit years)\n", result.TerminalRate*100, result.ForecastYears); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Market-Implied %dY CAGR:\t%.2f%%\n", result.ForecastYears, result.ImpliedGrowth*100); err != nil {
		return err
	}
	if result.HasHistoricalCAGR {
		if _, err := fmt.Fprintf(writer, "Historical CAGR:\t%.2f%%\nAlpha Spread (historical − implied):\t%+.2f%%\n", result.HistoricalCAGR*100, result.AlphaSpread*100); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "Implied Valuation Upside/Downside:\t%.1f%% (equity value at capped historical CAGR: %s)\n", result.MarginOfSafety*100, formatSummaryValue(result.IntrinsicAtHistCAGR, result.Unit)); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func WriteArbitrageTable(output io.Writer, candidates []ArbitrageCandidate) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "RANK\tTICKER\tCOMPANY\tSIC PEER\tSCORE\tROIC\tOWNER MGN\tSHARE RED.\tMARKET CAP\tEV/CF\tHIST g\tPEER MED\tIMPLIED g\tALPHA HIST\tALPHA PEER\tUPSIDE @ HIST"); err != nil {
		return err
	}
	usedRevenueFallback := false
	for index, candidate := range candidates {
		evToCashFlow := 0.0
		if candidate.Reverse.BaseCashFlow > 0 {
			evToCashFlow = candidate.Reverse.EnterpriseValue / candidate.Reverse.BaseCashFlow
		}
		historical := "n/a"
		if candidate.HasHistoricalGrowth {
			historical = fmt.Sprintf("%.1f%%", candidate.HistoricalGrowth*100)
			if candidate.HistoricalGrowthSource == "ranked revenue CAGR" {
				historical += "*"
				usedRevenueFallback = true
			}
		}
		peerMedian := "n/a"
		if candidate.HasPeerMedianGrowth {
			peerMedian = fmt.Sprintf("%.1f%%", candidate.PeerMedianGrowth*100)
		}
		alphaHistorical, alphaPeer := "n/a", "n/a"
		marginOfSafety := "n/a"
		if candidate.HasHistoricalGrowth {
			alphaHistorical = fmt.Sprintf("%+.1f%%", candidate.AlphaSpreadHist*100)
		}
		if candidate.HasPeerMedianGrowth {
			alphaPeer = fmt.Sprintf("%+.1f%%", candidate.AlphaSpreadPeer*100)
		}
		if candidate.Reverse.HasHistoricalCAGR {
			marginOfSafety = fmt.Sprintf("%.1f%%", candidate.Reverse.MarginOfSafety*100)
		}
		if _, err := fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%.1f\t%s\t%s\t%s\t%s\t%.1fx\t%s\t%s\t%.1f%%\t%s\t%s\t%s\n",
			index+1,
			candidate.Ranked.Ticker,
			candidate.Ranked.Company,
			candidate.Ranked.PeerGroup,
			candidate.Ranked.Score,
			formatPercent(candidate.Ranked.ROIC, candidate.Ranked.HasROIC),
			formatPercent(candidate.Ranked.OwnerEarningsMargin, candidate.Ranked.HasOwnerEarnings),
			formatPercent(candidate.Ranked.ShareReduction, candidate.Ranked.HasShareTrend),
			formatSummaryValue(candidate.Reverse.MarketCap, candidate.Reverse.Unit),
			evToCashFlow,
			historical,
			peerMedian,
			candidate.Reverse.ImpliedGrowth*100,
			alphaHistorical,
			alphaPeer,
			marginOfSafety,
		); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if usedRevenueFallback {
		_, err := fmt.Fprintln(output, "* Uses the ranked revenue CAGR because mode-specific historical cash-flow CAGR was unavailable.")
		return err
	}
	return nil
}

func formatPercent(value float64, present bool) string {
	if !present || !finite(value) {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", value*100)
}
