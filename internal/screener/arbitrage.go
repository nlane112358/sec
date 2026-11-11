package screener

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"text/tabwriter"

	"sec/internal/fundamentals"
	"sec/internal/store"
	"sec/internal/valuation"
)

type ArbitrageCandidate struct {
	Ranked                 RankedCompany
	Reverse                valuation.ReverseDCFResult
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
	DCFConfig           valuation.DCFConfig
}

// ScreenExpectationsArbitrage filters ranked companies to find those trading at
// valuations indicating a disconnect between market-implied growth and historical/peer performance.
func ScreenExpectationsArbitrage(ctx context.Context, s *store.Store, ranked []RankedCompany, marketCaps, prices map[string]float64, filter ArbitrageFilter) ([]ArbitrageCandidate, error) {
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

		summary, err := fundamentals.LoadFinancialSummary(ctx, s, 6, company.Ticker)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		marketCap := ResolveMarketCap(s, ctx, summary, company.Ticker, marketCaps, prices)
		if !finite(marketCap) || marketCap <= 0 {
			continue
		}

		config := filter.DCFConfig
		config.WACC = new(valuation.WACCConfig)
		*config.WACC = *filter.DCFConfig.WACC
		config.WACC.MarketCap = marketCap
		if config.WACC.DebtOverride == nil {
			config.WACC.DebtOverride = config.DebtOverride
		}
		if config.Mode == "" {
			config.Mode = valuation.DCFModeOwnerEarnings
		}
		reverse, err := valuation.SolveReverseDCF(summary, config)
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
			Ranked:                 company,
			Reverse:                reverse,
			HistoricalGrowth:       historicalGrowth,
			HistoricalGrowthSource: historicalSource,
			HasHistoricalGrowth:    hasHistorical,
			PeerMedianGrowth:       peerMedian,
			HasPeerMedianGrowth:    hasPeerMedian,
			ConservativeAnchor:     anchor,
			AlphaSpreadHist:        alphaHist,
			AlphaSpreadPeer:        alphaPeer,
			ConservativeSpread:     anchor - reverse.ImpliedGrowth,
			ExpectationsRatio:      expectationsRatio,
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

// WriteArbitrageTable prints the expectations arbitrage candidates in a formatted ASCII table.
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
