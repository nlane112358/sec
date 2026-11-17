package screener

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"sec/internal/fundamentals"
	"sec/internal/store"
)

func seedPrecomputeCompany(
	t *testing.T,
	s *store.Store,
	cik, companyName, sic string,
	tickers []string,
	years int,
	baseRev float64,
	revGrowthRate float64,
	rdVal float64,
) {
	t.Helper()
	ctx := context.Background()
	primaryTicker := tickers[0]
	for _, tk := range tickers {
		if err := s.SaveCompany(ctx, cik, companyName, tk, sic, "Test Industry"); err != nil {
			t.Fatalf("save company %s (%s): %v", cik, tk, err)
		}
	}

	nowYear := time.Now().Year()
	recentFilingDate := time.Now().AddDate(0, -1, 0).Format("2006-01-02")

	var facts []store.FinancialFact
	var filings []store.Filing

	for idx := 0; idx < years; idx++ {
		yr := nowYear - 1 - idx
		scale := math.Pow(1+revGrowthRate, float64(years-1-idx))
		rev := baseRev * scale
		opInc := rev * 0.22
		netInc := rev * 0.16
		ocf := rev * 0.24
		capex := rev * 0.04
		sbc := rev * 0.02
		equity := rev * 0.80
		cash := rev * 0.30
		debt := rev * 0.10
		assets := rev * 1.40
		rd := rdVal * scale
		sga := rev * 0.12
		tax := opInc * 0.21
		pretax := opInc

		acc := fmt.Sprintf("%s-%02d-000001", cik, yr%100)
		periodStart := fmt.Sprintf("%d-01-01", yr)
		periodEnd := fmt.Sprintf("%d-12-31", yr)
		filingDate := recentFilingDate
		if idx > 0 {
			filingDate = fmt.Sprintf("%d-02-15", yr+1)
		}

		filings = append(filings, store.Filing{
			CIK:             cik,
			Accession:       acc,
			Form:            "10-K",
			FilingDate:      filingDate,
			ReportDate:      periodEnd,
			PrimaryDocument: "form10k.htm",
			DocumentTitle:   "Form 10-K",
		})

		addDuration := func(concept, unit string, val float64) {
			facts = append(facts, store.FinancialFact{
				Company:    companyName,
				CIK:        cik,
				Ticker:     primaryTicker,
				Taxonomy:   "us-gaap",
				Concept:    concept,
				Label:      concept,
				Unit:       unit,
				Value:      val,
				Start:      periodStart,
				End:        periodEnd,
				FilingDate: filingDate,
				Form:       "10-K",
				Accession:  acc,
			})
		}
		addInstant := func(concept, unit string, val float64) {
			facts = append(facts, store.FinancialFact{
				Company:    companyName,
				CIK:        cik,
				Ticker:     primaryTicker,
				Taxonomy:   "us-gaap",
				Concept:    concept,
				Label:      concept,
				Unit:       unit,
				Value:      val,
				End:        periodEnd,
				FilingDate: filingDate,
				Form:       "10-K",
				Accession:  acc,
			})
		}

		addDuration("Revenues", "USD", rev)
		addDuration("OperatingIncomeLoss", "USD", opInc)
		addDuration("NetIncomeLoss", "USD", netInc)
		addDuration("NetCashProvidedByUsedInOperatingActivities", "USD", ocf)
		addDuration("PaymentsToAcquirePropertyPlantAndEquipment", "USD", capex)
		addDuration("ShareBasedCompensation", "USD", sbc)
		addDuration("ResearchAndDevelopmentExpense", "USD", rd)
		addDuration("SellingGeneralAndAdministrativeExpense", "USD", sga)
		addDuration("IncomeTaxExpenseBenefit", "USD", tax)
		addDuration("IncomeLossFromContinuingOperationsBeforeIncomeTaxesExtraordinaryItemsNoncontrollingInterest", "USD", pretax)
		addDuration("WeightedAverageNumberOfDilutedSharesOutstanding", "shares", 100_000_000)

		addInstant("StockholdersEquity", "USD", equity)
		addInstant("CashAndCashEquivalentsAtCarryingValue", "USD", cash)
		addInstant("LongTermDebtNoncurrent", "USD", debt)
		addInstant("Assets", "USD", assets)
		facts = append(facts, store.FinancialFact{
			Company:    companyName,
			CIK:        cik,
			Ticker:     primaryTicker,
			Taxonomy:   "dei",
			Concept:    "EntityCommonStockSharesOutstanding",
			Label:      "Shares Outstanding",
			Unit:       "shares",
			Value:      100_000_000,
			End:        periodEnd,
			FilingDate: filingDate,
			Form:       "10-K",
			Accession:  acc,
		})
	}

	if err := s.SaveFacts(ctx, primaryTicker, facts); err != nil {
		t.Fatalf("save facts %s: %v", primaryTicker, err)
	}
	if err := s.SaveFilings(ctx, filings); err != nil {
		t.Fatalf("save filings %s: %v", primaryTicker, err)
	}
}

func TestPrecomputeRankAndGAAPArbitrageEndToEnd(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer s.Close()

	// Seed 2 companies, one with two tickers sharing the same CIK (like GOOG/GOOGL).
	seedPrecomputeCompany(t, s, "0000001001", "Alpha Tech", "7372", []string{"ALPH", "ALPHB"}, 7, 1_000_000_000, 0.12, 180_000_000)
	seedPrecomputeCompany(t, s, "0000001002", "Beta Soft", "7372", []string{"BETA"}, 7, 800_000_000, 0.08, 120_000_000)

	// 1. Precompute full DB.
	rep, err := PrecomputeMissingOrDirty(ctx, s, PrecomputeOptions{
		Workers:           2,
		RefreshRankScores: true,
	})
	if err != nil {
		t.Fatalf("precompute failed: %v", err)
	}
	if rep.Recomputed != 2 {
		t.Fatalf("expected 2 unique CIKs recomputed, got %d", rep.Recomputed)
	}

	stats, err := s.PrecomputeStats(ctx, fundamentals.SummarySchemaVersion, RankSchemaVersion, GAAPArbitrageSchemaVersion)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalCompanies != 2 || stats.CleanSummaries != 2 || stats.Rank5Count != 2 || stats.Rank6Count != 2 || stats.Arb5Count != 2 {
		t.Fatalf("unexpected precompute stats: %+v", stats)
	}

	// Verify RankScores were persisted directly to sec_rank_variables in SQLite.
	var storedScore6 float64
	if err := s.DB().QueryRowContext(ctx, `SELECT score FROM sec_rank_variables WHERE cik = '0000001001' AND years = 6`).Scan(&storedScore6); err != nil {
		t.Fatalf("query stored rank score: %v", err)
	}
	if storedScore6 <= 0 {
		t.Fatalf("expected positive persisted 6y score for Alpha Tech, got %v", storedScore6)
	}

	// 2. Subsequent incremental run must do zero recomputations.
	rep2, err := PrecomputeMissingOrDirty(ctx, s, PrecomputeOptions{RefreshRankScores: true})
	if err != nil {
		t.Fatalf("second precompute: %v", err)
	}
	if rep2.CheckedStale != 0 || rep2.Recomputed != 0 {
		t.Fatalf("expected 0 stale/recomputed on clean DB, got %+v", rep2)
	}

	// 3. Compare RankCompanies (reading from precomputed table) against direct makeRankCandidate.
	rankRes, err := RankCompanies(ctx, s, 6, 100_000_000, []string{"ALPH", "ALPHB", "BETA", "MISSING"}, true)
	if err != nil {
		t.Fatalf("RankCompanies: %v", err)
	}
	if rankRes.Considered != 3 || len(rankRes.Companies) != 3 {
		t.Fatalf("expected 3 considered/ranked ticker rows (multi-ticker ALPH+ALPHB+BETA), got considered=%d companies=%d", rankRes.Considered, len(rankRes.Companies))
	}
	if len(rankRes.Missing) != 1 || rankRes.Missing[0] != "MISSING" {
		t.Fatalf("expected missing=[MISSING], got %v", rankRes.Missing)
	}

	alphSum, err := fundamentals.LoadFinancialSummary(ctx, s, 6, "ALPH")
	if err != nil {
		t.Fatalf("load summary ALPH: %v", err)
	}
	alphbSum, err := fundamentals.LoadFinancialSummary(ctx, s, 6, "ALPHB")
	if err != nil {
		t.Fatalf("load summary ALPHB: %v", err)
	}
	if alphbSum.Ticker != "ALPHB" {
		t.Fatalf("expected LoadFinancialSummary to preserve requested alternate ticker ALPHB, got %q", alphbSum.Ticker)
	}
	directCand, ok := makeRankCandidate(alphSum, 100_000_000, false)
	if !ok {
		t.Fatal("expected direct makeRankCandidate ok=true")
	}
	for _, c := range rankRes.Companies {
		if c.Ticker == "ALPH" {
			if math.Abs(c.Revenue-directCand.company.Revenue) > 1e-6 ||
				math.Abs(c.ROIC-directCand.company.ROIC) > 1e-9 ||
				math.Abs(c.RevenueCAGR-directCand.company.RevenueCAGR) > 1e-9 {
				t.Fatalf("precomputed rank mismatch vs direct: got %+v, want %+v", c, directCand.company)
			}
		}
	}

	// 4. Compare ScreenModernGAAPArbitrage against direct evaluateModernGAAPCompany.
	filter := ModernGAAPFilter{
		MinAdjustedROIC: 0.05,
		MinRevenue:      100_000_000,
		Years:           5,
		SortBy:          "score",
	}
	prices := map[string]float64{"ALPH": 50.0, "ALPHB": 48.0, "BETA": 20.0}
	arbRes, err := ScreenModernGAAPArbitrage(ctx, s, nil, nil, prices, filter)
	if err != nil {
		t.Fatalf("ScreenModernGAAPArbitrage: %v", err)
	}
	if len(arbRes) != 3 {
		t.Fatalf("expected 3 GAAP arbitrage ticker results, got %d", len(arbRes))
	}
	directArb, ok := evaluateModernGAAPCompany(ctx, s, store.LocalCompany{
		CIK: "0000001001", Name: "Alpha Tech", Ticker: "ALPHB", SIC: "7372", SICDescription: "Test Industry",
	}, filter, nil, prices)
	if !ok {
		t.Fatal("expected direct evaluateModernGAAPCompany ok=true")
	}
	for _, c := range arbRes {
		if c.Ticker == "ALPHB" {
			if math.Abs(c.MarketCap-directArb.MarketCap) > 1e-3 ||
				math.Abs(c.AdjustedROIC-directArb.AdjustedROIC) > 1e-9 ||
				math.Abs(c.ArbitrageScore-directArb.ArbitrageScore) > 1e-9 {
				t.Fatalf("precomputed GAAP arbitrage mismatch for ALPHB: got %+v, want %+v", c, directArb)
			}
		}
	}

	// 5. Incremental update when a new filing/fact arrives for BETA only.
	nextYear := time.Now().Year()
	nextAcc := fmt.Sprintf("0000001002-%02d-000099", nextYear%100)
	nextEnd := fmt.Sprintf("%d-12-31", nextYear)
	nextFilingDate := time.Now().Format("2006-01-02")
	newBetaRev := 2_500_000_000.0
	err = s.SaveFacts(ctx, "BETA", []store.FinancialFact{
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "Revenues", Label: "Revenues", Unit: "USD", Value: newBetaRev, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "OperatingIncomeLoss", Label: "OpInc", Unit: "USD", Value: 600_000_000, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "NetIncomeLoss", Label: "NetInc", Unit: "USD", Value: 450_000_000, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "NetCashProvidedByUsedInOperatingActivities", Label: "OCF", Unit: "USD", Value: 620_000_000, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "PaymentsToAcquirePropertyPlantAndEquipment", Label: "CapEx", Unit: "USD", Value: 80_000_000, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "ShareBasedCompensation", Label: "SBC", Unit: "USD", Value: 40_000_000, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "ResearchAndDevelopmentExpense", Label: "RD", Unit: "USD", Value: 350_000_000, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "SellingGeneralAndAdministrativeExpense", Label: "SGA", Unit: "USD", Value: 250_000_000, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "IncomeTaxExpenseBenefit", Label: "Tax", Unit: "USD", Value: 120_000_000, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "IncomeLossFromContinuingOperationsBeforeIncomeTaxesExtraordinaryItemsNoncontrollingInterest", Label: "Pretax", Unit: "USD", Value: 600_000_000, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "WeightedAverageNumberOfDilutedSharesOutstanding", Label: "Shares", Unit: "shares", Value: 100_000_000, Start: fmt.Sprintf("%d-01-01", nextYear), End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "StockholdersEquity", Label: "Equity", Unit: "USD", Value: 1_900_000_000, End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "CashAndCashEquivalentsAtCarryingValue", Label: "Cash", Unit: "USD", Value: 750_000_000, End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "LongTermDebtNoncurrent", Label: "Debt", Unit: "USD", Value: 200_000_000, End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
		{Company: "Beta Soft", CIK: "0000001002", Ticker: "BETA", Taxonomy: "us-gaap", Concept: "Assets", Label: "Assets", Unit: "USD", Value: 3_200_000_000, End: nextEnd, FilingDate: nextFilingDate, Form: "10-K", Accession: nextAcc},
	})
	if err != nil {
		t.Fatalf("save new beta facts: %v", err)
	}
	if err := s.SaveFilings(ctx, []store.Filing{{
		CIK: "0000001002", Accession: nextAcc, Form: "10-K", FilingDate: nextFilingDate, ReportDate: nextEnd, PrimaryDocument: "new10k.htm",
	}}); err != nil {
		t.Fatalf("save new beta filing: %v", err)
	}

	// Only BETA should be marked dirty; Alpha Tech remains clean.
	staleAfterNew, err := s.StaleCompanyCIKs(ctx, fundamentals.SummarySchemaVersion, RankSchemaVersion, GAAPArbitrageSchemaVersion, []int{5, 6}, []int{5}, nil, false)
	if err != nil {
		t.Fatalf("stale check after new BETA filing: %v", err)
	}
	if len(staleAfterNew) != 1 || staleAfterNew[0].CIK != "0000001002" {
		t.Fatalf("expected only BETA (0000001002) dirty, got %+v", staleAfterNew)
	}

	// RankCompanies should auto-refresh ONLY BETA and return updated revenue!
	rankUpdated, err := RankCompanies(ctx, s, 6, 100_000_000, nil, false)
	if err != nil {
		t.Fatalf("RankCompanies after BETA update: %v", err)
	}
	var betaRev float64
	for _, c := range rankUpdated.Companies {
		if c.Ticker == "BETA" {
			betaRev = c.Revenue
		}
	}
	if betaRev != newBetaRev {
		t.Fatalf("expected BETA rank revenue to update to %.0f, got %.0f", newBetaRev, betaRev)
	}

	// ScreenModernGAAPArbitrage should see BETA updated automatically as well.
	arbUpdated, err := ScreenModernGAAPArbitrage(ctx, s, []string{"BETA"}, nil, prices, filter)
	if err != nil {
		t.Fatalf("ScreenModernGAAPArbitrage after update: %v", err)
	}
	if len(arbUpdated) != 1 || arbUpdated[0].GAAPRevenue != newBetaRev {
		t.Fatalf("expected BETA GAAP revenue %.0f after update, got %+v", newBetaRev, arbUpdated)
	}
}
