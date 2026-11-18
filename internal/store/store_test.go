package store

import (
	"context"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open in-memory store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPrecomputeDirtyTriggersAndEpoch(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	cik := "0000000001"
	if err := s.SaveCompany(ctx, cik, "Acme Corp", "ACME", "3571", "Electronic Computers"); err != nil {
		t.Fatalf("save company: %v", err)
	}
	recentFilingDate := time.Now().AddDate(0, -1, 0).Format("2006-01-02")
	fact := FinancialFact{
		Company:    "Acme Corp",
		CIK:        cik,
		Ticker:     "ACME",
		Taxonomy:   "us-gaap",
		Concept:    "Revenues",
		Label:      "Revenue",
		Unit:       "USD",
		Value:      500_000_000,
		Start:      "2024-01-01",
		End:        "2024-12-31",
		FilingDate: recentFilingDate,
		Form:       "10-K",
		Accession:  "0000000001-25-000001",
	}
	if err := s.SaveFacts(ctx, "ACME", []FinancialFact{fact}); err != nil {
		t.Fatalf("save facts: %v", err)
	}
	filing := Filing{
		CIK:             cik,
		Accession:       "0000000001-25-000001",
		Form:            "10-K",
		FilingDate:      recentFilingDate,
		ReportDate:      "2024-12-31",
		PrimaryDocument: "acme10k.htm",
		DocumentTitle:   "Form 10-K",
	}
	if err := s.SaveFilings(ctx, []Filing{filing}); err != nil {
		t.Fatalf("save filings: %v", err)
	}

	// Initially no summary row exists, so StaleCompanyCIKs includes the company.
	stale, err := s.StaleCompanyCIKs(ctx, 1, 1, 1, []int{5, 6}, []int{5}, nil, false)
	if err != nil {
		t.Fatalf("stale query: %v", err)
	}
	if len(stale) != 1 || stale[0].CIK != cik {
		t.Fatalf("expected 1 stale company %s, got %+v", cik, stale)
	}

	// Save clean precomputed bundle.
	err = s.SaveCompanyPrecomputedBatch(ctx, []CompanyPrecomputedBundle{{
		Summary: &CompanySummaryRecord{
			CIK:                       cik,
			Ticker:                    "ACME",
			CompanyName:               "Acme Corp",
			SIC:                       "3571",
			Currency:                  "USD",
			LatestPeriodEnd:           "2024-12-31",
			PeriodsCount:              1,
			LatestFilingDate:          recentFilingDate,
			LatestAccession:           "0000000001-25-000001",
			LatestFinancialFilingDate: recentFilingDate,
			FilingsCount:              1,
			LatestShareCount:          10_000_000,
			HasLatestShareCount:       true,
			SummaryJSON:               "{}",
			SchemaVersion:             1,
		},
		RankVars: []RankVariableRecord{
			{CIK: cik, Years: 5, Ticker: "ACME", CompanyName: "Acme Corp", SIC: "3571", SchemaVersion: 1},
			{CIK: cik, Years: 6, Ticker: "ACME", CompanyName: "Acme Corp", SIC: "3571", SchemaVersion: 1},
		},
		ArbVars: []GAAPArbitrageVariableRecord{
			{CIK: cik, Years: 5, Ticker: "ACME", CompanyName: "Acme Corp", SIC: "3571", SchemaVersion: 1},
		},
	}})
	if err != nil {
		t.Fatalf("save precomputed batch: %v", err)
	}

	// Stale check should now be empty.
	stale, err = s.StaleCompanyCIKs(ctx, 1, 1, 1, []int{5, 6}, []int{5}, nil, false)
	if err != nil {
		t.Fatalf("stale query after save: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("expected 0 stale companies, got %+v", stale)
	}

	// Re-upserting identical company, fact, and filing must NOT mark dirty = 1.
	if err := s.SaveCompany(ctx, cik, "Acme Corp", "ACME", "3571", "Electronic Computers"); err != nil {
		t.Fatalf("re-save identical company: %v", err)
	}
	if err := s.SaveFacts(ctx, "ACME", []FinancialFact{fact}); err != nil {
		t.Fatalf("re-save identical fact: %v", err)
	}
	if err := s.SaveFilings(ctx, []Filing{filing}); err != nil {
		t.Fatalf("re-save identical filing: %v", err)
	}
	stale, err = s.StaleCompanyCIKs(ctx, 1, 1, 1, []int{5, 6}, []int{5}, nil, false)
	if err != nil {
		t.Fatalf("stale query after identical upsert: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("identical upsert should not mark dirty, got %+v", stale)
	}

	// Fast paths for LatestShareCount and HasRecentFinancialFiling should use clean summary row.
	if shares, ok := s.LatestShareCount(ctx, "ACME"); !ok || shares != 10_000_000 {
		t.Fatalf("expected cached share count 10000000, got %.0f (ok=%v)", shares, ok)
	}
	recent, err := s.HasRecentFinancialFiling(ctx, "ACME", time.Now().AddDate(0, -15, 0))
	if err != nil || !recent {
		t.Fatalf("expected HasRecentFinancialFiling=true, got %v err=%v", recent, err)
	}

	// Inserting a new filing via SaveFilings must mark dirty = 1 via SQLite trigger.
	newFilingDate := time.Now().Format("2006-01-02")
	if err := s.SaveFilings(ctx, []Filing{{
		CIK:             cik,
		Accession:       "0000000001-25-000002",
		Form:            "10-Q",
		FilingDate:      newFilingDate,
		ReportDate:      newFilingDate,
		PrimaryDocument: "acme10q.htm",
		DocumentTitle:   "Form 10-Q",
	}}); err != nil {
		t.Fatalf("save new filing: %v", err)
	}
	stale, err = s.StaleCompanyCIKs(ctx, 1, 1, 1, []int{5, 6}, []int{5}, nil, false)
	if err != nil {
		t.Fatalf("stale query after new filing: %v", err)
	}
	if len(stale) != 1 || stale[0].CIK != cik {
		t.Fatalf("expected new filing trigger to mark %s dirty, got %+v", cik, stale)
	}

	// CacheEpoch monotonically increments.
	epoch0, err := s.CacheEpoch(ctx)
	if err != nil || epoch0 != 0 {
		t.Fatalf("expected initial epoch 0, got %d err=%v", epoch0, err)
	}
	if err := s.BumpCacheEpoch(ctx); err != nil {
		t.Fatalf("bump epoch: %v", err)
	}
	epoch1, err := s.CacheEpoch(ctx)
	if err != nil || epoch1 != 1 {
		t.Fatalf("expected incremented epoch 1, got %d err=%v", epoch1, err)
	}
}

func TestSQLFactInsertMarksDirtyViaTrigger(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	cik := "0000000002"
	if err := s.SaveCompany(ctx, cik, "Beta Inc", "BETA", "7372", "Prepackaged Software"); err != nil {
		t.Fatalf("save company: %v", err)
	}
	err := s.SaveCompanySummaryCache(ctx, CompanySummaryRecord{
		CIK:           cik,
		Ticker:        "BETA",
		CompanyName:   "Beta Inc",
		SIC:           "7372",
		Currency:      "USD",
		SummaryJSON:   "{}",
		SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("save summary cache: %v", err)
	}
	if _, ok, _ := s.LoadCompanySummaryCache(ctx, cik, 1); !ok {
		t.Fatal("expected summary cache hit before fact insert")
	}

	// Raw SQL insert into sec_facts bypasses Store.SaveFacts and must still invalidate via trigger.
	_, err = s.DB().ExecContext(ctx, `
INSERT INTO sec_facts (cik, company_name, ticker, taxonomy, concept, label, unit, value, period_start, period_end, filing_date, form, accession_number, frame)
VALUES (?, 'Beta Inc', 'BETA', 'us-gaap', 'Revenues', 'Revenue', 'USD', 123456789, '2025-01-01', '2025-12-31', '2026-02-01', '10-K', '0000000002-26-000001', '')`, cik)
	if err != nil {
		t.Fatalf("raw sql insert fact: %v", err)
	}
	if _, ok, _ := s.LoadCompanySummaryCache(ctx, cik, 1); ok {
		t.Fatal("expected raw sql insert trigger to mark summary dirty and invalidate cache hit")
	}
}

func TestQuoteFXCacheAndHydrationStatus(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)

	if err := s.SaveQuotes(ctx, []QuoteRecord{
		{Ticker: "AAPL", Price: 225.5, High52: 240, Low52: 170, Currency: "USD", Source: "live", FetchedAt: now},
		{Ticker: "OLD", Price: 10.0, Currency: "USD", Source: "live", FetchedAt: old},
	}); err != nil {
		t.Fatalf("save quotes: %v", err)
	}
	q, ok, err := s.LoadQuote(ctx, "aapl")
	if err != nil || !ok || q.Price != 225.5 {
		t.Fatalf("load AAPL quote got (%+v, ok=%v, err=%v)", q, ok, err)
	}
	prunedQuotes, err := s.PruneQuotes(ctx, now.Add(-24*time.Hour))
	if err != nil || prunedQuotes != 1 {
		t.Fatalf("prune quotes got (%d, %v), want 1", prunedQuotes, err)
	}
	if _, ok, _ := s.LoadQuote(ctx, "OLD"); ok {
		t.Fatal("expected OLD quote to be pruned")
	}

	if err := s.SaveFXRate(ctx, FXRateRecord{Currency: "EUR", RateToUSD: 1.09, Source: "live", FetchedAt: now}); err != nil {
		t.Fatalf("save FX rate: %v", err)
	}
	if err := s.SaveFXRate(ctx, FXRateRecord{Currency: "JPY", RateToUSD: 0.0067, Source: "live", FetchedAt: old}); err != nil {
		t.Fatalf("save old FX rate: %v", err)
	}
	fx, ok, err := s.LoadFXRate(ctx, "eur")
	if err != nil || !ok || fx.RateToUSD != 1.09 {
		t.Fatalf("load EUR FX got (%+v, ok=%v, err=%v)", fx, ok, err)
	}
	prunedFX, err := s.PruneFXRates(ctx, now.Add(-24*time.Hour))
	if err != nil || prunedFX != 1 {
		t.Fatalf("prune FX got (%d, %v), want 1", prunedFX, err)
	}

	// Empty shell company (0 facts) vs hydrated company (>0 facts)
	if err := s.SaveCompany(ctx, "0000000010", "Empty Shell Corp", "EMPT", "7372", "Software"); err != nil {
		t.Fatalf("save empty company: %v", err)
	}
	if err := s.SaveCompany(ctx, "0000000011", "Hydrated Corp", "HYDR", "3571", "Hardware"); err != nil {
		t.Fatalf("save hydrated company: %v", err)
	}
	if err := s.SaveFacts(ctx, "HYDR", []FinancialFact{{
		Company: "Hydrated Corp", CIK: "0000000011", Ticker: "HYDR",
		Taxonomy: "us-gaap", Concept: "Revenues", Label: "Revenues", Unit: "USD",
		Value: 1000, Start: "2025-01-01", End: "2025-12-31", FilingDate: "2026-02-01",
		Form: "10-K", Accession: "0000000011-26-000001",
	}}); err != nil {
		t.Fatalf("save fact: %v", err)
	}
	if err := s.SaveFilings(ctx, []Filing{{
		CIK: "0000000011", Accession: "0000000011-26-000002", Form: "10-Q",
		FilingDate: "2026-10-25", ReportDate: "2026-09-30", PrimaryDocument: "q3.htm",
	}}); err != nil {
		t.Fatalf("save filing: %v", err)
	}

	stEmpt, err := s.CompanyHydrationStatus(ctx, "EMPT")
	if err != nil || stEmpt.HasFacts {
		t.Fatalf("expected EMPT HasFacts=false, got %+v err=%v", stEmpt, err)
	}
	stHydr, err := s.CompanyHydrationStatus(ctx, "HYDR")
	if err != nil || !stHydr.HasFacts || stHydr.LatestForm != "10-Q" {
		t.Fatalf("expected HYDR HasFacts=true LatestForm=10-Q, got %+v err=%v", stHydr, err)
	}
	batchStatus, err := s.CompanyHydrationStatuses(ctx, []string{"EMPT", "HYDR", "MISS"})
	if err != nil {
		t.Fatalf("batch hydration statuses: %v", err)
	}
	if batchStatus["EMPT"].HasFacts || !batchStatus["HYDR"].HasFacts || batchStatus["MISS"].CIK != "" {
		t.Fatalf("unexpected batch hydration statuses: %+v", batchStatus)
	}

	prunedEmpty, err := s.PruneEmptyCompanies(ctx)
	if err != nil || prunedEmpty != 1 {
		t.Fatalf("prune empty companies got (%d, %v), want 1", prunedEmpty, err)
	}
	if err := s.Vacuum(ctx); err != nil {
		t.Fatalf("vacuum: %v", err)
	}
}
