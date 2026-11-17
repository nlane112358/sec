package screener

import (
	"context"
	"encoding/json"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"sec/internal/fundamentals"
	"sec/internal/store"
)

const (
	RankSchemaVersion          = 1
	GAAPArbitrageSchemaVersion = 1
)

var (
	DefaultPrecomputeRankYears = []int{5, 6}
	DefaultPrecomputeArbYears  = []int{5}
)

type PrecomputeOptions struct {
	Tickers           []string
	RankYears         []int
	ArbYears          []int
	Force             bool
	Workers           int
	BatchSize         int
	RefreshRankScores bool
	OnProgress        func(PrecomputeProgress)
}

type PrecomputeProgress struct {
	Total     int
	Completed int
	CIK       string
	Ticker    string
}

type PrecomputeReport struct {
	CheckedStale int
	Recomputed   int
	ScoresUpdated int
	Duration     time.Duration
}

// PrecomputeMissingOrDirty incrementally computes and saves company summaries,
// rank variables, and GAAP arbitrage variables for any missing, dirty, or
// schema-outdated companies, then optionally refreshes full-universe rank scores
// and bumps sec_meta.cache_epoch when data changed.
func PrecomputeMissingOrDirty(ctx context.Context, s *store.Store, opts PrecomputeOptions) (PrecomputeReport, error) {
	start := time.Now()
	var report PrecomputeReport

	rankYears := opts.RankYears
	if len(rankYears) == 0 && opts.ArbYears == nil {
		rankYears = DefaultPrecomputeRankYears
	}
	arbYears := opts.ArbYears
	if opts.RankYears == nil && len(arbYears) == 0 {
		arbYears = DefaultPrecomputeArbYears
	}

	stale, err := s.StaleCompanyCIKs(
		ctx,
		fundamentals.SummarySchemaVersion,
		RankSchemaVersion,
		GAAPArbitrageSchemaVersion,
		rankYears,
		arbYears,
		opts.Tickers,
		opts.Force,
	)
	if err != nil {
		return report, err
	}
	report.CheckedStale = len(stale)

	if len(stale) > 0 {
		// Whenever we compute/refresh a company bundle, also populate standard
		// rank (5, 6) and arbitrage (5) windows so switching commands needs no
		// extra pass over sec_facts.
		bundleRankYears := unionYears(rankYears, DefaultPrecomputeRankYears)
		bundleArbYears := unionYears(arbYears, DefaultPrecomputeArbYears)

		workers := opts.Workers
		if workers <= 0 {
			workers = runtime.NumCPU()
			if workers > 6 {
				workers = 6
			}
			if workers < 2 {
				workers = 2
			}
		}
		batchSize := opts.BatchSize
		if batchSize <= 0 {
			batchSize = 50
		}

		type jobResult struct {
			bundle  store.CompanyPrecomputedBundle
			company store.LocalCompany
			err     error
		}

		workCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		jobs := make(chan store.LocalCompany, len(stale))
		results := make(chan jobResult, workers*4)

		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for company := range jobs {
					if workCtx.Err() != nil {
						return
					}
					bundle, buildErr := buildCompanyPrecomputedBundle(workCtx, s, company, bundleRankYears, bundleArbYears, opts.Force)
					select {
					case results <- jobResult{bundle: bundle, company: company, err: buildErr}:
					case <-workCtx.Done():
						return
					}
				}
			}()
		}

		go func() {
			defer close(jobs)
			for _, company := range stale {
				select {
				case jobs <- company:
				case <-workCtx.Done():
					return
				}
			}
		}()

		go func() {
			wg.Wait()
			close(results)
		}()

		batch := make([]store.CompanyPrecomputedBundle, 0, batchSize)
		completed := 0
		for res := range results {
			if res.err != nil {
				cancel()
				for range results {
				}
				return report, res.err
			}
			batch = append(batch, res.bundle)
			completed++
			if len(batch) >= batchSize {
				if err := s.SaveCompanyPrecomputedBatch(ctx, batch); err != nil {
					cancel()
					for range results {
					}
					return report, err
				}
				report.Recomputed += len(batch)
				batch = batch[:0]
			}
			if opts.OnProgress != nil {
				opts.OnProgress(PrecomputeProgress{
					Total:     len(stale),
					Completed: completed,
					CIK:       res.company.CIK,
					Ticker:    res.company.Ticker,
				})
			}
		}
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if len(batch) > 0 {
			if err := s.SaveCompanyPrecomputedBatch(ctx, batch); err != nil {
				return report, err
			}
			report.Recomputed += len(batch)
		}
	}

	if opts.RefreshRankScores && (report.Recomputed > 0 || opts.Force) {
		scoreYears := unionYears(rankYears, DefaultPrecomputeRankYears)
		for _, y := range scoreYears {
			if y != 5 && y != 6 {
				continue
			}
			n, err := RefreshFullUniverseRankScores(ctx, s, y)
			if err != nil {
				return report, err
			}
			report.ScoresUpdated += n
		}
	}

	if report.Recomputed > 0 || report.ScoresUpdated > 0 {
		if err := s.BumpCacheEpoch(ctx); err != nil {
			return report, err
		}
	}
	report.Duration = time.Since(start)
	return report, nil
}

// PrecomputeCompany recomputes all saved variables for one CIK immediately after
// new filings/facts are synced for that company.
func PrecomputeCompany(ctx context.Context, s *store.Store, company store.LocalCompany) error {
	bundle, err := buildCompanyPrecomputedBundle(ctx, s, company, DefaultPrecomputeRankYears, DefaultPrecomputeArbYears, true)
	if err != nil {
		return err
	}
	if err := s.SaveCompanyPrecomputedBatch(ctx, []store.CompanyPrecomputedBundle{bundle}); err != nil {
		return err
	}
	return s.BumpCacheEpoch(ctx)
}

// RefreshFullUniverseRankScores computes peer-group percentile scores across all
// currently clean, eligible companies in the database for a given years window
// and writes score & peer_group back to sec_rank_variables.
func RefreshFullUniverseRankScores(ctx context.Context, s *store.Store, years int) (int, error) {
	if years < 5 || years > 6 {
		return 0, nil
	}
	rows, err := s.LoadRankVariables(ctx, years, RankSchemaVersion, nil)
	if err != nil {
		return 0, err
	}
	cutoffDate := time.Now().AddDate(0, -15, 0).Format("2006-01-02")
	active := make([]rankCandidate, 0, len(rows))
	activeCIKs := make([]string, 0, len(rows))
	seenInactive := make(map[string]struct{})
	var updates []store.RankScoreUpdate

	for _, row := range rows {
		isRecent := row.LatestFinancialFilingDate != "" && row.LatestFinancialFilingDate >= cutoffDate
		if !row.Eligible || !isRecent {
			if _, ok := seenInactive[row.CIK]; !ok {
				seenInactive[row.CIK] = struct{}{}
				updates = append(updates, store.RankScoreUpdate{
					CIK:           row.CIK,
					Years:         years,
					PeerGroup:     row.RawPeerGroup,
					Score:         0,
					SchemaVersion: RankSchemaVersion,
				})
			}
			continue
		}
		active = append(active, candidateFromRankRecord(row))
		activeCIKs = append(activeCIKs, row.CIK)
	}

	broadenSmallPeerGroups(active, 5)
	assignRankPercentiles(active)

	seenActive := make(map[string]struct{}, len(active))
	for i, cand := range active {
		cik := activeCIKs[i]
		if _, ok := seenActive[cik]; ok {
			continue
		}
		seenActive[cik] = struct{}{}
		updates = append(updates, store.RankScoreUpdate{
			CIK:           cik,
			Years:         years,
			PeerGroup:     cand.company.PeerGroup,
			Score:         cand.company.Score,
			SchemaVersion: RankSchemaVersion,
		})
	}
	if err := s.UpdateRankScoresBatch(ctx, years, RankSchemaVersion, updates); err != nil {
		return 0, err
	}
	return len(updates), nil
}

func buildCompanyPrecomputedBundle(
	ctx context.Context,
	s *store.Store,
	company store.LocalCompany,
	rankYears []int,
	arbYears []int,
	forceSummary bool,
) (store.CompanyPrecomputedBundle, error) {
	var bundle store.CompanyPrecomputedBundle
	var summaryRec store.CompanySummaryRecord
	var fullSummary fundamentals.FinancialSummary
	var err error

	if !forceSummary {
		if cachedRec, ok, cacheErr := s.LoadCompanySummaryCache(ctx, company.CIK, fundamentals.SummarySchemaVersion); cacheErr == nil && ok {
			if jsonErr := json.Unmarshal([]byte(cachedRec.SummaryJSON), &fullSummary); jsonErr == nil {
				summaryRec = cachedRec
				goto buildVars
			}
		}
	}

	summaryRec, fullSummary, err = fundamentals.BuildCompanySummaryRecord(ctx, s, company.CIK, company.Name, company.Ticker, company.SIC)
	if err != nil {
		return bundle, err
	}
	bundle.Summary = &summaryRec

buildVars:
	for _, y := range rankYears {
		if y < 5 || y > 6 {
			continue
		}
		sliced := fundamentals.SliceSummary(fullSummary, y, company.Ticker)
		rv := buildRankVariableRecord(company, y, sliced, summaryRec.LatestFinancialFilingDate)
		bundle.RankVars = append(bundle.RankVars, rv)
	}
	for _, y := range arbYears {
		if y < 1 || y > 10 {
			continue
		}
		sliced := fundamentals.SliceSummary(fullSummary, y, company.Ticker)
		av := buildGAAPArbitrageVariableRecord(
			company,
			y,
			sliced,
			summaryRec.LatestFinancialFilingDate,
			summaryRec.LatestShareCount,
			summaryRec.HasLatestShareCount,
		)
		bundle.ArbVars = append(bundle.ArbVars, av)
	}
	return bundle, nil
}

func buildRankVariableRecord(
	company store.LocalCompany,
	years int,
	summary fundamentals.FinancialSummary,
	latestFinFilingDate string,
) store.RankVariableRecord {
	sector := sectorFromSIC(company.SIC)
	rawPeerGroup := peerGroupFromSIC(company.SIC)
	rec := store.RankVariableRecord{
		CIK:                       company.CIK,
		Years:                     years,
		Ticker:                    company.Ticker,
		CompanyName:               company.Name,
		SIC:                       company.SIC,
		Sector:                    sector,
		RawPeerGroup:              rawPeerGroup,
		PeerGroup:                 rawPeerGroup,
		LatestFinancialFilingDate: latestFinFilingDate,
		Currency:                  fundamentals.SummaryCurrency(summary),
		SchemaVersion:             RankSchemaVersion,
	}
	if rec.Currency == "" {
		rec.Currency = "USD"
	}

	financial := sector == "Financials"
	cand, ok := makeRankCandidate(summary, 0, financial)
	if !ok {
		return rec
	}

	_, hasRevGrowth := cand.factors["growth"]
	rec.Eligible = true
	rec.Year = cand.company.Year
	rec.TrendYears = cand.company.TrendYears
	rec.Revenue = cand.company.Revenue
	rec.Currency = cand.company.Currency
	rec.ROIC = cand.company.ROIC
	rec.HasROIC = cand.company.HasROIC
	rec.ROE = cand.company.ROE
	rec.HasROE = cand.company.HasROE
	rec.OperatingMargin = cand.company.OperatingMargin
	rec.OperatingConsistency = cand.company.OperatingConsistency
	rec.HasOperatingMargin = cand.company.HasOperatingMargin
	rec.FCFMargin = cand.company.FCFMargin
	rec.OwnerEarningsMargin = cand.company.OwnerEarningsMargin
	rec.HasOwnerEarnings = cand.company.HasOwnerEarnings
	rec.HasSBCAdjustment = cand.company.HasSBCAdjustment
	rec.RevenueCAGR = cand.company.RevenueCAGR
	rec.HasRevenueGrowth = hasRevGrowth
	rec.EPSGrowth = cand.company.EPSGrowth
	rec.HasEPSGrowth = cand.company.HasEPSGrowth
	rec.BalanceSheetQuality = cand.company.BalanceSheetQuality
	rec.HasBalanceSheet = cand.company.HasBalanceSheet
	rec.BookValueGrowth = cand.company.BookValueGrowth
	rec.HasBookValueGrowth = cand.company.HasBookValueGrowth
	rec.ShareReduction = cand.company.ShareReduction
	rec.HasShareTrend = cand.company.HasShareTrend
	rec.SplitAdjusted = cand.company.SplitAdjusted
	return rec
}

func candidateFromRankRecord(r store.RankVariableRecord) rankCandidate {
	factors := make(map[string]float64, 8)
	if r.HasRevenueGrowth {
		factors["growth"] = r.RevenueCAGR
	}
	if r.HasEPSGrowth {
		factors["eps_growth"] = r.EPSGrowth
	}
	if r.HasShareTrend && finite(r.ShareReduction) {
		factors["shares"] = r.ShareReduction
	}
	if r.Sector == "Financials" {
		if r.HasROE {
			factors["roe"] = r.ROE
		}
		if r.HasBookValueGrowth {
			factors["book_growth"] = r.BookValueGrowth
		}
	} else {
		if r.HasROIC {
			factors["roic"] = r.ROIC
		}
		if r.HasOperatingMargin {
			factors["op_margin"] = r.OperatingMargin
			factors["op_consistency"] = r.OperatingConsistency
		}
		if r.HasOwnerEarnings {
			factors["owner_earnings"] = r.OwnerEarningsMargin
		}
		if r.HasBalanceSheet {
			factors["balance"] = r.BalanceSheetQuality
		}
	}
	return rankCandidate{
		company: RankedCompany{
			Ticker:               r.Ticker,
			Company:              r.CompanyName,
			Year:                 r.Year,
			TrendYears:           r.TrendYears,
			Revenue:              r.Revenue,
			Currency:             r.Currency,
			ROIC:                 r.ROIC,
			HasROIC:              r.HasROIC,
			ROE:                  r.ROE,
			HasROE:               r.HasROE,
			OperatingMargin:      r.OperatingMargin,
			OperatingConsistency: r.OperatingConsistency,
			HasOperatingMargin:   r.HasOperatingMargin,
			FCFMargin:            r.FCFMargin,
			OwnerEarningsMargin:  r.OwnerEarningsMargin,
			HasOwnerEarnings:     r.HasOwnerEarnings,
			HasSBCAdjustment:     r.HasSBCAdjustment,
			RevenueCAGR:          r.RevenueCAGR,
			EPSGrowth:            r.EPSGrowth,
			HasEPSGrowth:         r.HasEPSGrowth,
			BalanceSheetQuality:  r.BalanceSheetQuality,
			HasBalanceSheet:      r.HasBalanceSheet,
			BookValueGrowth:      r.BookValueGrowth,
			HasBookValueGrowth:   r.HasBookValueGrowth,
			ShareReduction:       r.ShareReduction,
			HasShareTrend:        r.HasShareTrend,
			SplitAdjusted:        r.SplitAdjusted,
			Sector:               r.Sector,
			PeerGroup:            r.RawPeerGroup,
		},
		factors:    factors,
		percentile: make(map[string]float64, len(factors)),
	}
}

func buildGAAPArbitrageVariableRecord(
	company store.LocalCompany,
	years int,
	summary fundamentals.FinancialSummary,
	latestFinFilingDate string,
	latestShareCount float64,
	hasLatestShares bool,
) store.GAAPArbitrageVariableRecord {
	sector := sectorFromSIC(company.SIC)
	rec := store.GAAPArbitrageVariableRecord{
		CIK:                       company.CIK,
		Years:                     years,
		Ticker:                    company.Ticker,
		CompanyName:               company.Name,
		SIC:                       company.SIC,
		SICDescription:            company.SICDescription,
		Sector:                    sector,
		LatestFinancialFilingDate: latestFinFilingDate,
		SchemaVersion:             GAAPArbitrageSchemaVersion,
	}

	currency := fundamentals.SummaryCurrency(summary)
	if currency == "" {
		currency = "USD"
	}
	rec.Currency = currency

	if len(summary.Periods) == 0 {
		return rec
	}

	revenueMetric := fundamentals.NamedMetric(summary, "Revenue")
	if len(revenueMetric) == 0 || !revenueMetric[0].Present || revenueMetric[0].Value <= 0 {
		return rec
	}

	intCapMetric := fundamentals.NamedMetric(summary, "Intangible Capital")
	adjEBITMetric := fundamentals.NamedMetric(summary, "Adjusted Operating Income")
	adjROICMetric := fundamentals.NamedMetric(summary, "Intangible-Adjusted ROIC")
	amortMetric := fundamentals.NamedMetric(summary, "Amortization of Intangibles")

	if len(intCapMetric) == 0 || !intCapMetric[0].Present || intCapMetric[0].Value <= 0 {
		return rec
	}
	if len(adjEBITMetric) == 0 || !adjEBITMetric[0].Present || adjEBITMetric[0].Value <= 0 {
		return rec
	}

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
	for i := 0; i < len(adjROICMetric) && i < years; i++ {
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

	sharesOut := 0.0
	for _, metricName := range []string{"Shares Outstanding", "Diluted Weighted Average Shares"} {
		values := fundamentals.NamedMetric(summary, metricName)
		for index := 0; index < len(summary.Periods) && index < len(values); index++ {
			val := values[index]
			if fundamentals.IsShareCount(val) && finite(val.Value) && val.Value > 0 {
				sharesOut = val.Value
				break
			}
		}
		if sharesOut > 0 {
			break
		}
	}
	if sharesOut <= 0 && hasLatestShares && finite(latestShareCount) && latestShareCount > 0 {
		sharesOut = latestShareCount
	}

	baseSignal, baseScore := computeArbitrageSignalAndScore(
		gaapNetIncome, latestAdjEBIT, latestAdjROIC, hasAdjROIC,
		hasGAAPROIC, roicDelta, surging, 0, false, 0, false,
	)

	rec.Eligible = true
	rec.GAAPRevenue = gaapRevenue
	rec.GAAPNetIncome = gaapNetIncome
	rec.GAAPOperatingIncome = gaapOpIncome
	rec.GAAPOperatingMargin = gaapOpMargin
	rec.GAAPROIC = gaapROIC
	rec.HasGAAPROIC = hasGAAPROIC
	rec.GAAPUnprofitable = gaapUnprofitable
	rec.IntangibleCapital = latestIntCap
	rec.AdjustedEBIT = latestAdjEBIT
	rec.AdjustedEBITMargin = latestAdjEBITMargin
	rec.AdjustedInvestedCap = latestAdjInvestedCap
	rec.AdjustedROIC = latestAdjROIC
	rec.HasAdjustedROIC = hasAdjROIC
	rec.AvgAdjustedROIC = avgAdjROIC
	rec.ROICDelta = roicDelta
	rec.SurgingROIC = surging
	rec.OwnerEarnings = ownerBase
	rec.HasOwnerEarnings = hasOwner
	rec.AdjustedOwnerEarnings = adjustedOwnerEarnings
	rec.SharesOutstanding = sharesOut
	rec.BaseArbitrageScore = baseScore
	rec.BaseArbitrageSignal = baseSignal
	return rec
}

func computeArbitrageSignalAndScore(
	gaapNetIncome, latestAdjEBIT, latestAdjROIC float64,
	hasAdjROIC, hasGAAPROIC bool,
	roicDelta float64,
	surging bool,
	gaapPE float64,
	hasGAAP_PE bool,
	ownerEarningsYield float64,
	hasYield bool,
) (string, float64) {
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
	return signal, score
}

func unionYears(a, b []int) []int {
	seen := make(map[int]struct{}, len(a)+len(b))
	out := make([]int, 0, len(a)+len(b))
	for _, y := range a {
		if y > 0 {
			if _, ok := seen[y]; !ok {
				seen[y] = struct{}{}
				out = append(out, y)
			}
		}
	}
	for _, y := range b {
		if y > 0 {
			if _, ok := seen[y]; !ok {
				seen[y] = struct{}{}
				out = append(out, y)
			}
		}
	}
	sort.Ints(out)
	return out
}

// ParseTickersFromUniverse normalizes a ticker slice by trimming and uppercasing.
func NormalizeTickers(tickers []string) []string {
	out := make([]string, 0, len(tickers))
	for _, t := range tickers {
		clean := strings.ToUpper(strings.TrimSpace(t))
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out
}
