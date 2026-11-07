package sec

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type RankedCompany struct {
	Ticker               string
	Company              string
	Score                float64
	Year                 string
	TrendYears           int
	Revenue              float64
	ROIC                 float64
	HasROIC              bool
	ROE                  float64
	HasROE               bool
	OperatingMargin      float64
	OperatingConsistency float64
	HasOperatingMargin   bool
	FCFMargin            float64
	OwnerEarningsMargin  float64
	HasOwnerEarnings     bool
	HasSBCAdjustment     bool
	RevenueCAGR          float64
	EPSGrowth            float64
	HasEPSGrowth         bool
	BalanceSheetQuality  float64
	HasBalanceSheet      bool
	BookValueGrowth      float64
	HasBookValueGrowth   bool
	ShareReduction       float64
	HasShareTrend        bool
	SplitAdjusted        bool
	Sector               string
	PeerGroup            string
}

type RankResult struct {
	Companies            []RankedCompany
	Considered           int
	Excluded             int
	Missing              []string
	FinancialIncluded    int
	FinancialExcluded    int
	NonfinancialIncluded int
	UnclassifiedIncluded int
	SICUnavailable       int
	SBCAdjusted          int
	SBCFallback          int
	MissingSolvencyData  int
}

type rankCandidate struct {
	company    RankedCompany
	factors    map[string]float64
	percentile map[string]float64
}

type rankFactor struct {
	name   string
	weight float64
}

var nonfinancialRankFactors = []rankFactor{
	{name: "roic", weight: 0.20},
	{name: "growth", weight: 0.15},
	{name: "op_margin", weight: 0.075},
	{name: "op_consistency", weight: 0.075},
	{name: "owner_earnings", weight: 0.20},
	{name: "balance", weight: 0.10},
	{name: "eps_growth", weight: 0.10},
	{name: "shares", weight: 0.10},
}

// Financial companies use a separate score because industrial FCF and ROIC
// are not comparable to deposit, lending, insurance-float, or investment flows.
var financialRankFactors = []rankFactor{
	{name: "roe", weight: 0.30},
	{name: "growth", weight: 0.20},
	{name: "book_growth", weight: 0.20},
	{name: "eps_growth", weight: 0.20},
	{name: "shares", weight: 0.10},
}

// RankCompanies screens saved companies and calculates percentiles within SEC
// SIC major-group cohorts. years is the number of annual periods to load; six
// periods are needed to calculate five-year trends.
func (s *Store) RankCompanies(ctx context.Context, years int, minimumRevenue float64, universe []string, excludeFinancials ...bool) (RankResult, error) {
	var result RankResult
	if years < 5 || years > 6 {
		return result, fmt.Errorf("rank years must be 5 or 6 annual periods (6 periods provide five-year trends)")
	}
	if minimumRevenue < 0 || math.IsNaN(minimumRevenue) || math.IsInf(minimumRevenue, 0) {
		return result, fmt.Errorf("minimum revenue must be a finite non-negative USD amount")
	}
	companies, err := s.Companies(ctx)
	if err != nil {
		return result, err
	}
	requested := make(map[string]struct{}, len(universe))
	for _, ticker := range universe {
		requested[strings.ToUpper(strings.TrimSpace(ticker))] = struct{}{}
	}
	found := make(map[string]struct{}, len(companies))
	candidates := make([]rankCandidate, 0, len(companies))
	excludeFin := len(excludeFinancials) > 0 && excludeFinancials[0]
	for _, company := range companies {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if len(requested) > 0 {
			if _, ok := requested[strings.ToUpper(company.Ticker)]; !ok {
				continue
			}
		}
		found[strings.ToUpper(company.Ticker)] = struct{}{}
		result.Considered++
		sector := sectorFromSIC(company.SIC)
		if sector == "Unclassified" {
			result.SICUnavailable++
		}
		if sector == "Financials" && excludeFin {
			result.Excluded++
			result.FinancialExcluded++
			continue
		}
		recent, err := s.hasRecentFinancialFiling(ctx, company.Ticker, time.Now().AddDate(0, -15, 0))
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			result.Excluded++
			continue
		}
		if !recent {
			result.Excluded++
			continue
		}
		summary, err := s.FinancialSummary(ctx, years, company.Ticker)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			result.Excluded++
			continue
		}
		financial := sector == "Financials"
		candidate, ok := makeRankCandidate(summary, minimumRevenue, financial)
		if !ok {
			result.Excluded++
			continue
		}
		candidate.company.Sector = sector
		candidate.company.PeerGroup = peerGroupFromSIC(company.SIC)
		if sector == "Financials" {
			result.FinancialIncluded++
		} else if sector == "Nonfinancial" {
			result.NonfinancialIncluded++
		} else {
			result.UnclassifiedIncluded++
		}
		if candidate.company.HasSBCAdjustment {
			result.SBCAdjusted++
		} else if candidate.company.HasOwnerEarnings {
			result.SBCFallback++
		}
		if !financial && !candidate.company.HasBalanceSheet {
			result.MissingSolvencyData++
		}
		candidates = append(candidates, candidate)
	}
	for ticker := range requested {
		if _, ok := found[ticker]; !ok {
			result.Missing = append(result.Missing, ticker)
		}
	}
	sort.Strings(result.Missing)
	broadenSmallPeerGroups(candidates, 5)
	assignRankPercentiles(candidates)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].company.Score != candidates[j].company.Score {
			return candidates[i].company.Score > candidates[j].company.Score
		}
		return candidates[i].company.Ticker < candidates[j].company.Ticker
	})
	result.Companies = make([]RankedCompany, len(candidates))
	for i, candidate := range candidates {
		result.Companies[i] = candidate.company
	}
	return result, nil
}

// Small SIC major groups have unstable percentiles (a one-company group can
// only receive a midpoint score), so fold groups with fewer than the minimum
// peer count into a broader SIC division or, for finance, the financial cohort.
func broadenSmallPeerGroups(candidates []rankCandidate, minimumPeers int) {
	if minimumPeers < 2 {
		return
	}
	counts := make(map[string]int)
	for _, candidate := range candidates {
		counts[candidate.company.PeerGroup]++
	}
	for i := range candidates {
		company := &candidates[i].company
		if counts[company.PeerGroup] >= minimumPeers {
			continue
		}
		if company.Sector == "Financials" {
			company.PeerGroup = "Financials"
			continue
		}
		if strings.HasPrefix(company.PeerGroup, "SIC ") {
			majorGroup := strings.TrimPrefix(company.PeerGroup, "SIC ")
			if len(majorGroup) > 0 {
				company.PeerGroup = "SIC division " + majorGroup[:1]
			}
		}
	}
}

func sectorFromSIC(raw string) string {
	code, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return "Unclassified"
	}
	// SEC SIC division H spans finance, insurance, and real estate. This is a
	// broad proxy for financial companies, not an official GICS classification.
	if code >= 6000 && code <= 6799 {
		return "Financials"
	}
	return "Nonfinancial"
}

func peerGroupFromSIC(raw string) string {
	code, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || code < 0 || code > 9999 {
		return "Unclassified"
	}
	majorGroup := code / 100
	if code >= 6000 && code <= 6799 {
		return fmt.Sprintf("Financial SIC %02d", majorGroup)
	}
	return fmt.Sprintf("SIC %02d", majorGroup)
}

func (s *Store) hasRecentFinancialFiling(ctx context.Context, ticker string, cutoff time.Time) (bool, error) {
	cik, _, _, err := s.companyForTicker(ctx, ticker)
	if err != nil {
		return false, err
	}
	var exists int
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(
    SELECT 1 FROM sec_filings
    WHERE cik = ? AND filing_date >= ?
      AND form IN ('10-K', '10-K/A', '10-Q', '10-Q/A', '20-F', '20-F/A', '40-F', '40-F/A', '6-K', '6-K/A')
)`, cik, cutoff.Format("2006-01-02")).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check recent filing for %s: %w", ticker, err)
	}
	return exists != 0, nil
}

func makeRankCandidate(summary FinancialSummary, minimumRevenue float64, financialCompany ...bool) (rankCandidate, bool) {
	financial := len(financialCompany) > 0 && financialCompany[0]
	trendYears := len(summary.Periods) - 1
	if trendYears > 5 {
		trendYears = 5
	}
	if trendYears < 4 || !consecutiveAnnualPeriods(summary.Periods, trendYears+1) {
		return rankCandidate{}, false
	}
	revenue := namedMetric(summary, "Revenue")
	netIncome := namedMetric(summary, "Net Income")
	operatingCashFlow := namedMetric(summary, "Operating Cash Flow")
	operatingIncome := namedMetric(summary, "Operating Income")
	freeCashFlow := namedMetric(summary, "Free Cash Flow")
	ownerEarningsMetric := namedMetric(summary, "SBC-Adjusted FCF Proxy")
	sbc := namedMetric(summary, "Stock-Based Compensation")
	equity := namedMetric(summary, "Stockholders' Equity")
	cash := namedMetric(summary, "Cash & Equivalents")
	debt := namedMetric(summary, "Debt / Borrowings")
	assets := namedMetric(summary, "Total Assets")
	taxExpense := namedMetric(summary, "Income Tax Expense")
	pretaxIncome := namedMetric(summary, "Income Before Tax")
	shares, splitAdjusted := adjustSplitShareSeries(namedMetric(summary, "Diluted Weighted Average Shares"))
	if len(revenue) < trendYears+1 || len(netIncome) < trendYears+1 || len(shares) < trendYears+1 {
		return rankCandidate{}, false
	}
	if !isUSD(revenue[0]) || revenue[0].Value < minimumRevenue || revenue[0].Value <= 0 ||
		!isUSD(revenue[trendYears]) || revenue[trendYears].Value <= 0 ||
		!isShareCount(shares[0]) || !isShareCount(shares[trendYears]) ||
		shares[0].Value <= 0 || shares[trendYears].Value <= 0 {
		return rankCandidate{}, false
	}
	for i := 0; i < trendYears+1; i++ {
		if !isUSD(revenue[i]) || revenue[i].Value <= 0 || !isShareCount(shares[i]) || shares[i].Value <= 0 {
			return rankCandidate{}, false
		}
	}
	for i := 0; i < 3; i++ {
		if i >= len(netIncome) || !isUSD(netIncome[i]) || netIncome[i].Value <= 0 {
			return rankCandidate{}, false
		}
	}
	if !financial && (len(operatingCashFlow) == 0 || !isUSD(operatingCashFlow[0]) || operatingCashFlow[0].Value <= 0) {
		return rankCandidate{}, false
	}

	latestCash, cashOK := latestUSD(cash)
	latestDebt, debtOK := latestUSD(debt)
	latestAssets, assetsOK := latestUSD(assets)
	ownerValues := make([]float64, 0, trendYears)
	ownerMargins := make([]float64, 0, trendYears)
	adjustedCount := 0
	for i := 0; i < trendYears; i++ {
		owner := SummaryValue{}
		if i < len(ownerEarningsMetric) && isUSD(ownerEarningsMetric[i]) {
			owner = ownerEarningsMetric[i]
			if i < len(sbc) && isUSD(sbc[i]) && strings.EqualFold(sbc[i].Unit, owner.Unit) {
				adjustedCount++
			}
		} else if i < len(freeCashFlow) && isUSD(freeCashFlow[i]) {
			owner = freeCashFlow[i]
		}
		if owner.Present && isUSD(revenue[i]) && strings.EqualFold(owner.Unit, revenue[i].Unit) {
			ownerValues = append(ownerValues, owner.Value)
			ownerMargins = append(ownerMargins, owner.Value/revenue[i].Value)
		}
	}
	ownerEarnings := 0.0
	ownerEarningsMargin, hasOwnerEarnings := meanAvailable(ownerMargins, 3)
	if len(ownerValues) > 0 {
		ownerEarnings = mean(ownerValues)
	}
	hasSBCAdjustment := adjustedCount > 0

	// Financial businesses have structurally unusual cash flows and leverage,
	// so they are screened on profitability without an industrial solvency test.
	if !financial && cashOK && debtOK && hasOwnerEarnings && !(latestDebt.Value-latestCash.Value < 3*ownerEarnings || latestCash.Value >= latestDebt.Value) {
		return rankCandidate{}, false
	}

	revenueCAGR, hasRevenueGrowth := compoundGrowth(revenue[0].Value, revenue[trendYears].Value, trendYears)
	epsStart := netIncome[trendYears].Value / shares[trendYears].Value
	epsEnd := netIncome[0].Value / shares[0].Value
	epsGrowth, hasEPSGrowth := compoundGrowth(epsEnd, epsStart, trendYears)
	shareReduction := (shares[trendYears].Value - shares[0].Value) / shares[trendYears].Value

	roe, hasROE := averageROE(netIncome, equity, trendYears)
	roic, hasROIC := averageROIC(operatingIncome, equity, debt, cash, taxExpense, pretaxIncome, trendYears)
	bookGrowth, hasBookGrowth := bookValuePerShareGrowth(equity, shares, trendYears)

	opMarginValues := make([]float64, 0, trendYears)
	positiveMarginYears := 0
	for i := 0; i < trendYears && i < len(operatingIncome); i++ {
		if isUSD(operatingIncome[i]) && isUSD(revenue[i]) && strings.EqualFold(operatingIncome[i].Unit, revenue[i].Unit) {
			margin := operatingIncome[i].Value / revenue[i].Value
			opMarginValues = append(opMarginValues, margin)
			if margin > 0 {
				positiveMarginYears++
			}
		}
	}
	operatingMargin, hasOperatingMargin := meanAvailable(opMarginValues, 3)
	operatingConsistency := 0.0
	if hasOperatingMargin {
		operatingConsistency = float64(positiveMarginYears) / float64(len(opMarginValues))
	}

	fcfMargins := make([]float64, 0, trendYears)
	for i := 0; i < trendYears && i < len(freeCashFlow); i++ {
		if isUSD(freeCashFlow[i]) && isUSD(revenue[i]) && strings.EqualFold(freeCashFlow[i].Unit, revenue[i].Unit) {
			fcfMargins = append(fcfMargins, freeCashFlow[i].Value/revenue[i].Value)
		}
	}
	fcfMargin, _ := meanAvailable(fcfMargins, 1)

	balanceQuality, hasBalanceQuality := 0.0, false
	if cashOK && debtOK && assetsOK && latestAssets.Value > 0 {
		balanceQuality = (latestCash.Value - latestDebt.Value) / latestAssets.Value
		hasBalanceQuality = finite(balanceQuality)
	}

	factors := make(map[string]float64)
	if hasRevenueGrowth {
		factors["growth"] = revenueCAGR
	}
	if hasEPSGrowth {
		factors["eps_growth"] = epsGrowth
	}
	if finite(shareReduction) {
		factors["shares"] = shareReduction
	}
	if financial {
		if hasROE {
			factors["roe"] = roe
		}
		if hasBookGrowth {
			factors["book_growth"] = bookGrowth
		}
	} else {
		if hasROIC {
			factors["roic"] = roic
		}
		if hasOperatingMargin {
			factors["op_margin"] = operatingMargin
			factors["op_consistency"] = operatingConsistency
		}
		if hasOwnerEarnings {
			factors["owner_earnings"] = ownerEarningsMargin
		}
		if hasBalanceQuality {
			factors["balance"] = balanceQuality
		}
	}
	if len(factors) == 0 {
		return rankCandidate{}, false
	}
	return rankCandidate{
		company: RankedCompany{
			Ticker:               summary.Ticker,
			Company:              summary.Company,
			Year:                 summary.Periods[0].Year,
			TrendYears:           trendYears,
			Revenue:              revenue[0].Value,
			ROIC:                 roic,
			HasROIC:              hasROIC,
			ROE:                  roe,
			HasROE:               hasROE,
			OperatingMargin:      operatingMargin,
			OperatingConsistency: operatingConsistency,
			HasOperatingMargin:   hasOperatingMargin,
			FCFMargin:            fcfMargin,
			OwnerEarningsMargin:  ownerEarningsMargin,
			HasOwnerEarnings:     hasOwnerEarnings,
			HasSBCAdjustment:     hasSBCAdjustment,
			RevenueCAGR:          revenueCAGR,
			EPSGrowth:            epsGrowth,
			HasEPSGrowth:         hasEPSGrowth,
			BalanceSheetQuality:  balanceQuality,
			HasBalanceSheet:      hasBalanceQuality,
			BookValueGrowth:      bookGrowth,
			HasBookValueGrowth:   hasBookGrowth,
			ShareReduction:       shareReduction,
			HasShareTrend:        true,
			SplitAdjusted:        splitAdjusted,
		},
		factors:    factors,
		percentile: make(map[string]float64),
	}, true
}

// adjustSplitShareSeries restates older annual weighted-average share counts
// when the discontinuity is within 6% of a common split or reverse-split ratio.
// Summary fact selection already prefers later filings, which often carry
// restated comparatives; this handles older periods outside those comparatives.
func adjustSplitShareSeries(values []SummaryValue) ([]SummaryValue, bool) {
	adjusted := append([]SummaryValue(nil), values...)
	commonSplitRatios := []float64{2, 3, 4, 5, 6, 7, 8, 10, 12, 15, 20, 25, 30, 40, 50, 100}
	changed := false
	for newer := len(adjusted) - 2; newer >= 0; newer-- {
		if !isShareCount(adjusted[newer]) || !isShareCount(adjusted[newer+1]) || adjusted[newer+1].Value <= 0 {
			continue
		}
		ratio := adjusted[newer].Value / adjusted[newer+1].Value
		factor := 0.0
		scaleOlderDown := false
		bestDistance := math.Inf(1)
		for _, split := range commonSplitRatios {
			for _, candidate := range []struct {
				ratio float64
				down  bool
			}{{split, false}, {1 / split, true}} {
				distance := math.Abs(math.Log(ratio / candidate.ratio))
				if distance < bestDistance {
					bestDistance = distance
					factor = split
					scaleOlderDown = candidate.down
				}
			}
		}
		if bestDistance > math.Log(1.06) {
			continue
		}
		for older := newer + 1; older < len(adjusted); older++ {
			if !isShareCount(adjusted[older]) {
				continue
			}
			if scaleOlderDown {
				adjusted[older].Value /= factor
			} else {
				adjusted[older].Value *= factor
			}
		}
		changed = true
	}
	return adjusted, changed
}

func averageROE(netIncome, equity []SummaryValue, years int) (float64, bool) {
	if years < 1 || len(netIncome) < years || len(equity) < years+1 {
		return 0, false
	}
	values := make([]float64, 0, years)
	for i := 0; i < years; i++ {
		if !isUSD(netIncome[i]) || !isUSD(equity[i]) || !isUSD(equity[i+1]) ||
			!strings.EqualFold(netIncome[i].Unit, equity[i].Unit) ||
			!strings.EqualFold(equity[i].Unit, equity[i+1].Unit) ||
			equity[i].Value <= 0 || equity[i+1].Value <= 0 {
			return 0, false
		}
		values = append(values, netIncome[i].Value/((equity[i].Value+equity[i+1].Value)/2))
	}
	value := mean(values)
	return value, finite(value)
}

func averageROIC(operatingIncome, equity, debt, cash, taxExpense, pretaxIncome []SummaryValue, years int) (float64, bool) {
	if years < 1 || len(operatingIncome) < years || len(equity) < years+1 || len(debt) < years+1 || len(cash) < years+1 {
		return 0, false
	}
	values := make([]float64, 0, years)
	for i := 0; i < years; i++ {
		if !isUSD(operatingIncome[i]) || !isUSD(equity[i]) || !isUSD(equity[i+1]) ||
			!isUSD(debt[i]) || !isUSD(debt[i+1]) || !isUSD(cash[i]) || !isUSD(cash[i+1]) {
			return 0, false
		}
		if !sameCurrency(operatingIncome[i], equity[i], debt[i], cash[i], equity[i+1], debt[i+1], cash[i+1]) {
			return 0, false
		}
		capitalStart := equity[i].Value + debt[i].Value - cash[i].Value
		capitalEnd := equity[i+1].Value + debt[i+1].Value - cash[i+1].Value
		investedCapital := (capitalStart + capitalEnd) / 2
		if investedCapital <= 0 {
			return 0, false
		}
		taxRate := 0.21
		if i < len(taxExpense) && i < len(pretaxIncome) && isUSD(taxExpense[i]) && isUSD(pretaxIncome[i]) &&
			strings.EqualFold(taxExpense[i].Unit, pretaxIncome[i].Unit) && pretaxIncome[i].Value > 0 {
			observed := math.Abs(taxExpense[i].Value) / pretaxIncome[i].Value
			if observed >= 0 && observed <= 0.50 {
				taxRate = observed
			}
		}
		values = append(values, operatingIncome[i].Value*(1-taxRate)/investedCapital)
	}
	value := mean(values)
	return value, finite(value)
}

func bookValuePerShareGrowth(equity, shares []SummaryValue, years int) (float64, bool) {
	if len(equity) < years+1 || len(shares) < years+1 {
		return 0, false
	}
	if !isUSD(equity[0]) || !isUSD(equity[years]) || !isShareCount(shares[0]) || !isShareCount(shares[years]) ||
		equity[0].Value <= 0 || equity[years].Value <= 0 || shares[0].Value <= 0 || shares[years].Value <= 0 {
		return 0, false
	}
	return compoundGrowth(equity[0].Value/shares[0].Value, equity[years].Value/shares[years].Value, years)
}

func compoundGrowth(latest, oldest float64, years int) (float64, bool) {
	if years < 1 || latest <= 0 || oldest <= 0 || !finite(latest) || !finite(oldest) {
		return 0, false
	}
	growth := math.Pow(latest/oldest, 1/float64(years)) - 1
	return growth, finite(growth)
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func meanAvailable(values []float64, minimum int) (float64, bool) {
	if len(values) < minimum {
		return 0, false
	}
	value := mean(values)
	return value, finite(value)
}

func latestUSD(values []SummaryValue) (SummaryValue, bool) {
	if len(values) == 0 || !isUSD(values[0]) {
		return SummaryValue{}, false
	}
	return values[0], true
}

func sameCurrency(values ...SummaryValue) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values[1:] {
		if !strings.EqualFold(value.Unit, values[0].Unit) {
			return false
		}
	}
	return true
}

func consecutiveAnnualPeriods(periods []SummaryPeriod, requested ...int) bool {
	count := 4
	if len(requested) > 0 {
		count = requested[0]
	}
	if count < 2 || len(periods) < count {
		return false
	}
	var newestYear int
	for i := 0; i < count; i++ {
		year, err := strconv.Atoi(periods[i].Year)
		if err != nil {
			return false
		}
		if i == 0 {
			newestYear = year
		}
		if year != newestYear-i {
			return false
		}
	}
	return true
}

func namedMetric(summary FinancialSummary, name string) []SummaryValue {
	for _, metric := range summary.Metrics {
		if metric.Name == name {
			return metric.Values
		}
	}
	return nil
}

func isUSD(value SummaryValue) bool {
	return value.Present && strings.EqualFold(value.Unit, "USD")
}

func isShareCount(value SummaryValue) bool {
	return value.Present && strings.EqualFold(value.Unit, "shares")
}

func assignRankPercentiles(candidates []rankCandidate) {
	groups := make(map[string][]int)
	for i, candidate := range candidates {
		groups[candidate.company.PeerGroup] = append(groups[candidate.company.PeerGroup], i)
	}
	for _, indices := range groups {
		financial := len(indices) > 0 && candidates[indices[0]].company.Sector == "Financials"
		factors := nonfinancialRankFactors
		if financial {
			factors = financialRankFactors
		}
		for _, factor := range factors {
			eligible := make([]int, 0, len(indices))
			for _, index := range indices {
				if _, ok := candidates[index].factors[factor.name]; ok {
					eligible = append(eligible, index)
				}
			}
			sort.Slice(eligible, func(i, j int) bool {
				return candidates[eligible[i]].factors[factor.name] < candidates[eligible[j]].factors[factor.name]
			})
			for start := 0; start < len(eligible); {
				end := start + 1
				for end < len(eligible) && candidates[eligible[end]].factors[factor.name] == candidates[eligible[start]].factors[factor.name] {
					end++
				}
				percentile := 0.5
				if len(eligible) > 1 {
					percentile = float64(start+end-1) / (2 * float64(len(eligible)-1))
				}
				for i := start; i < end; i++ {
					candidates[eligible[i]].percentile[factor.name] = percentile
				}
				start = end
			}
		}
		for _, index := range indices {
			var score, availableWeight float64
			for _, factor := range factors {
				percentile, ok := candidates[index].percentile[factor.name]
				if !ok {
					continue
				}
				score += factor.weight * percentile
				availableWeight += factor.weight
			}
			if availableWeight > 0 {
				candidates[index].company.Score = 100 * score / availableWeight
			}
		}
	}
}
