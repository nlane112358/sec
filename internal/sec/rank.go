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
	Ticker          string
	Company         string
	Score           float64
	Year            string
	Revenue         float64
	OperatingMargin float64
	FCFMargin       float64
	RevenueCAGR     float64
	CashConversion  float64
	ShareReduction  float64
}

type RankResult struct {
	Companies  []RankedCompany
	Considered int
	Excluded   int
	Missing    []string
}

type rankCandidate struct {
	company   RankedCompany
	fcfPct    float64
	growthPct float64
	cashPct   float64
	sharesPct float64
}

// RankCompanies applies a USD fundamentals screen and ranks eligible saved
// companies by percentile across four quality and growth factors.
func (s *Store) RankCompanies(ctx context.Context, years int, minimumRevenue float64, universe []string) (RankResult, error) {
	var result RankResult
	if years < 4 || years > 5 {
		return result, fmt.Errorf("rank years must be 4 or 5")
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
		candidate, ok := makeRankCandidate(summary, minimumRevenue)
		if !ok {
			result.Excluded++
			continue
		}
		candidates = append(candidates, candidate)
	}
	for ticker := range requested {
		if _, ok := found[ticker]; !ok {
			result.Missing = append(result.Missing, ticker)
		}
	}
	sort.Strings(result.Missing)
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

func makeRankCandidate(summary FinancialSummary, minimumRevenue float64) (rankCandidate, bool) {
	if len(summary.Periods) < 4 {
		return rankCandidate{}, false
	}
	if !consecutiveAnnualPeriods(summary.Periods[:4]) {
		return rankCandidate{}, false
	}
	revenue := namedMetric(summary, "Revenue")
	operatingIncome := namedMetric(summary, "Operating Income")
	netIncome := namedMetric(summary, "Net Income")
	operatingCashFlow := namedMetric(summary, "Operating Cash Flow")
	freeCashFlow := namedMetric(summary, "Free Cash Flow")
	cash := namedMetric(summary, "Cash & Equivalents")
	debt := namedMetric(summary, "Debt / Borrowings")
	shares := namedMetric(summary, "Shares Outstanding")
	if len(revenue) < 4 || len(netIncome) < 3 || len(operatingCashFlow) == 0 || len(freeCashFlow) < 3 || len(shares) < 4 {
		return rankCandidate{}, false
	}
	if !isUSD(revenue[0]) || !isUSD(revenue[3]) || revenue[0].Value < minimumRevenue || revenue[0].Value <= 0 || revenue[3].Value <= 0 {
		return rankCandidate{}, false
	}
	if !isUSD(operatingCashFlow[0]) || operatingCashFlow[0].Value <= 0 || !isUSD(netIncome[0]) || netIncome[0].Value <= 0 {
		return rankCandidate{}, false
	}
	if !isUSD(freeCashFlow[0]) || !isUSD(freeCashFlow[1]) || !isUSD(freeCashFlow[2]) {
		return rankCandidate{}, false
	}
	if !isUSD(revenue[1]) || !isUSD(revenue[2]) || revenue[1].Value <= 0 || revenue[2].Value <= 0 {
		return rankCandidate{}, false
	}
	if !isUSD(netIncome[1]) || !isUSD(netIncome[2]) || netIncome[1].Value <= 0 || netIncome[2].Value <= 0 {
		return rankCandidate{}, false
	}
	cashValue, debtValue := 0.0, 0.0
	if len(cash) > 0 && cash[0].Present {
		if !isUSD(cash[0]) {
			return rankCandidate{}, false
		}
		cashValue = cash[0].Value
	}
	if len(debt) > 0 && debt[0].Present {
		if !isUSD(debt[0]) {
			return rankCandidate{}, false
		}
		debtValue = debt[0].Value
	}
	if !(debtValue-cashValue < 3*freeCashFlow[0].Value || cashValue >= debtValue) {
		return rankCandidate{}, false
	}
	if !isShareCount(shares[0]) || !isShareCount(shares[3]) || shares[0].Value <= 0 || shares[3].Value <= 0 {
		return rankCandidate{}, false
	}

	fcfMargin, cashConversion, operatingMargin := 0.0, 0.0, 0.0
	operatingMarginCount := 0
	for i := 0; i < 3; i++ {
		fcfMargin += freeCashFlow[i].Value / revenue[i].Value
		cashConversion += freeCashFlow[i].Value / netIncome[i].Value
		if i < len(operatingIncome) && isUSD(operatingIncome[i]) {
			operatingMargin += operatingIncome[i].Value / revenue[i].Value
			operatingMarginCount++
		}
	}
	fcfMargin /= 3
	cashConversion /= 3
	if operatingMarginCount > 0 {
		operatingMargin /= float64(operatingMarginCount)
	}
	revenueCAGR := math.Pow(revenue[0].Value/revenue[3].Value, 1.0/3.0) - 1
	shareReduction := (shares[3].Value - shares[0].Value) / shares[3].Value
	for _, value := range []float64{fcfMargin, cashConversion, revenueCAGR, shareReduction, operatingMargin} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return rankCandidate{}, false
		}
	}
	return rankCandidate{
		company: RankedCompany{
			Ticker:          summary.Ticker,
			Company:         summary.Company,
			Year:            summary.Periods[0].Year,
			Revenue:         revenue[0].Value,
			OperatingMargin: operatingMargin,
			FCFMargin:       fcfMargin,
			RevenueCAGR:     revenueCAGR,
			CashConversion:  cashConversion,
			ShareReduction:  shareReduction,
		},
	}, true
}

func consecutiveAnnualPeriods(periods []SummaryPeriod) bool {
	if len(periods) < 4 {
		return false
	}
	var newestYear int
	for i := 0; i < 4; i++ {
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
	assign := func(value func(rankCandidate) float64, set func(*rankCandidate, float64)) {
		indices := make([]int, len(candidates))
		for i := range indices {
			indices[i] = i
		}
		sort.Slice(indices, func(i, j int) bool {
			return value(candidates[indices[i]]) < value(candidates[indices[j]])
		})
		for start := 0; start < len(indices); {
			end := start + 1
			for end < len(indices) && value(candidates[indices[end]]) == value(candidates[indices[start]]) {
				end++
			}
			percentile := 0.5
			if len(indices) > 1 {
				percentile = float64(start+end-1) / (2 * float64(len(indices)-1))
			}
			for i := start; i < end; i++ {
				set(&candidates[indices[i]], percentile)
			}
			start = end
		}
	}
	assign(func(c rankCandidate) float64 { return c.company.FCFMargin }, func(c *rankCandidate, p float64) { c.fcfPct = p })
	assign(func(c rankCandidate) float64 { return c.company.RevenueCAGR }, func(c *rankCandidate, p float64) { c.growthPct = p })
	assign(func(c rankCandidate) float64 { return c.company.CashConversion }, func(c *rankCandidate, p float64) { c.cashPct = p })
	assign(func(c rankCandidate) float64 { return c.company.ShareReduction }, func(c *rankCandidate, p float64) { c.sharesPct = p })
	for i := range candidates {
		candidates[i].company.Score = 100 * (0.35*candidates[i].fcfPct +
			0.35*candidates[i].growthPct + 0.15*candidates[i].cashPct +
			0.15*candidates[i].sharesPct)
	}
}
