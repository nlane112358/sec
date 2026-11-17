package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sec/internal/screener"
	"sec/internal/store"
)

func seedAPITestDB(t *testing.T, dbPath string) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer s.Close()

	cik := "0000005555"
	if err := s.SaveCompany(ctx, cik, "API Test Corp", "APIT", "7372", "Software"); err != nil {
		t.Fatalf("save company: %v", err)
	}
	nowYr := time.Now().Year()
	recentFiling := time.Now().AddDate(0, -1, 0).Format("2006-01-02")
	var facts []store.FinancialFact
	var filings []store.Filing
	for i := 0; i < 6; i++ {
		yr := nowYr - 1 - i
		acc := fmt.Sprintf("%s-%02d-000001", cik, yr%100)
		start := fmt.Sprintf("%d-01-01", yr)
		end := fmt.Sprintf("%d-12-31", yr)
		fdate := recentFiling
		if i > 0 {
			fdate = fmt.Sprintf("%d-02-15", yr+1)
		}
		filings = append(filings, store.Filing{
			CIK: cik, Accession: acc, Form: "10-K", FilingDate: fdate, ReportDate: end, PrimaryDocument: "k.htm",
		})
		for _, item := range []struct {
			c, u, s string
			v       float64
		}{
			{"Revenues", "USD", start, 500_000_000},
			{"OperatingIncomeLoss", "USD", start, 120_000_000},
			{"NetIncomeLoss", "USD", start, 90_000_000},
			{"NetCashProvidedByUsedInOperatingActivities", "USD", start, 130_000_000},
			{"ResearchAndDevelopmentExpense", "USD", start, 60_000_000},
			{"WeightedAverageNumberOfDilutedSharesOutstanding", "shares", start, 50_000_000},
			{"StockholdersEquity", "USD", "", 300_000_000},
		} {
			facts = append(facts, store.FinancialFact{
				Company: "API Test Corp", CIK: cik, Ticker: "APIT", Taxonomy: "us-gaap",
				Concept: item.c, Label: item.c, Unit: item.u, Value: item.v,
				Start: item.s, End: end, FilingDate: fdate, Form: "10-K", Accession: acc,
			})
		}
	}
	if err := s.SaveFacts(ctx, "APIT", facts); err != nil {
		t.Fatalf("save facts: %v", err)
	}
	if err := s.SaveFilings(ctx, filings); err != nil {
		t.Fatalf("save filings: %v", err)
	}
	if _, err := screener.PrecomputeMissingOrDirty(ctx, s, screener.PrecomputeOptions{RefreshRankScores: true}); err != nil {
		t.Fatalf("precompute: %v", err)
	}
}

func TestScreenerEndpointsAndEpochInvalidation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sec.db")
	seedAPITestDB(t, dbPath)

	srv, err := NewServer(dbPath)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	handler := srv.Handler()

	// 1. /api/screener/rank
	req := httptest.NewRequest("GET", "/api/screener/rank", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rank status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rankRes screener.RankResult
	if err := json.Unmarshal(rec.Body.Bytes(), &rankRes); err != nil {
		t.Fatalf("unmarshal rank JSON: %v", err)
	}
	if len(rankRes.Companies) != 1 || rankRes.Companies[0].Ticker != "APIT" {
		t.Fatalf("unexpected rank companies: %+v", rankRes.Companies)
	}

	// 2. /api/screener/gaap-arbitrage
	reqArb := httptest.NewRequest("GET", "/api/screener/gaap-arbitrage", nil)
	recArb := httptest.NewRecorder()
	handler.ServeHTTP(recArb, reqArb)
	if recArb.Code != http.StatusOK {
		t.Fatalf("gaap-arbitrage status=%d body=%s", recArb.Code, recArb.Body.String())
	}
	var arbPayload struct {
		Candidates []screener.ModernGAAPCandidate `json:"candidates"`
		Total      int                            `json:"total"`
	}
	if err := json.Unmarshal(recArb.Body.Bytes(), &arbPayload); err != nil {
		t.Fatalf("unmarshal gaap-arbitrage JSON: %v", err)
	}
	if arbPayload.Total != 1 || arbPayload.Candidates[0].Ticker != "APIT" {
		t.Fatalf("unexpected gaap-arbitrage candidates: %+v", arbPayload)
	}

	// 3. /api/screener/discount (exercises s.qaadMu locking & rankEpoch matching)
	reqDisc := httptest.NewRequest("GET", "/api/screener/discount", nil)
	recDisc := httptest.NewRecorder()
	handler.ServeHTTP(recDisc, reqDisc)
	if recDisc.Code != http.StatusOK {
		t.Fatalf("discount status=%d body=%s", recDisc.Code, recDisc.Body.String())
	}

	// 3b. /api/screener/owner-yield (exercises s.ownerYieldMu locking & epoch matching)
	reqYield := httptest.NewRequest("GET", "/api/screener/owner-yield", nil)
	recYield := httptest.NewRecorder()
	handler.ServeHTTP(recYield, reqYield)
	if recYield.Code != http.StatusOK {
		t.Fatalf("owner-yield status=%d body=%s", recYield.Code, recYield.Body.String())
	}
	var yieldPayload screener.OwnerYieldResult
	if err := json.Unmarshal(recYield.Body.Bytes(), &yieldPayload); err != nil {
		t.Fatalf("unmarshal owner-yield JSON: %v", err)
	}
	if yieldPayload.HurdleRate != screener.DefaultHurdleRate {
		t.Fatalf("unexpected hurdle rate: %f", yieldPayload.HurdleRate)
	}

	// 4. Simulate external sec sync modifying revenue and bumping cache_epoch
	ctx := context.Background()
	nowYr := time.Now().Year()
	nextAcc := fmt.Sprintf("0000005555-%02d-000099", (nowYr-1)%100)
	if err := srv.store.SaveFacts(ctx, "APIT", []store.FinancialFact{{
		Company: "API Test Corp", CIK: "0000005555", Ticker: "APIT", Taxonomy: "us-gaap",
		Concept: "Revenues", Label: "Revenues", Unit: "USD", Value: 999_000_000,
		Start: fmt.Sprintf("%d-01-01", nowYr-1), End: fmt.Sprintf("%d-12-31", nowYr-1),
		FilingDate: time.Now().Format("2006-01-02"), Form: "10-K", Accession: nextAcc,
	}}); err != nil {
		t.Fatalf("save updated fact: %v", err)
	}
	if err := srv.store.BumpCacheEpoch(ctx); err != nil {
		t.Fatalf("bump epoch: %v", err)
	}

	req2 := httptest.NewRequest("GET", "/api/screener/rank", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("rank after bump status=%d", rec2.Code)
	}
	var rankRes2 screener.RankResult
	if err := json.Unmarshal(rec2.Body.Bytes(), &rankRes2); err != nil {
		t.Fatalf("unmarshal rank2: %v", err)
	}
	if len(rankRes2.Companies) != 1 || rankRes2.Companies[0].Revenue != 999_000_000 {
		t.Fatalf("expected refreshed revenue 999000000 after CacheEpoch bump, got %+v", rankRes2.Companies)
	}
}

func TestRealDBScreenerEndpointsLatency(t *testing.T) {
	if os.Getenv("SEC_TEST_REAL_DB") != "1" {
		t.Skip("set SEC_TEST_REAL_DB=1 to benchmark API endpoints against ../../data/sec.db")
	}
	srv, err := NewServer("../../data/sec.db")
	if err != nil {
		t.Fatalf("NewServer real db: %v", err)
	}
	defer srv.Close()
	handler := srv.Handler()

	for _, endpoint := range []string{"/api/screener/rank", "/api/screener/owner-yield", "/api/screener/gaap-arbitrage", "/api/screener/discount"} {
		start := time.Now()
		req := httptest.NewRequest("GET", endpoint, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		elapsed := time.Since(start)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", endpoint, rec.Code, rec.Body.String())
		}
		if !json.Valid(rec.Body.Bytes()) {
			t.Fatalf("%s returned invalid JSON", endpoint)
		}
		t.Logf("%s -> %d bytes in %s", endpoint, rec.Body.Len(), elapsed)
	}

	// Verify details of Owner Yield rankings
	req := httptest.NewRequest("GET", "/api/screener/owner-yield", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var result screener.OwnerYieldResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal owner yield result: %v", err)
	}
	if result.Total == 0 {
		t.Fatalf("expected candidates in real DB, got 0")
	}
	if result.HurdleRate != screener.DefaultHurdleRate {
		t.Errorf("expected hurdle rate %f, got %f", screener.DefaultHurdleRate, result.HurdleRate)
	}
	if len(result.Candidates) > 1 {
		if result.Candidates[0].OwnerYield < result.Candidates[1].OwnerYield {
			t.Errorf("expected candidate 0 yield (%f) >= candidate 1 yield (%f)",
				result.Candidates[0].OwnerYield, result.Candidates[1].OwnerYield)
		}
		if result.Candidates[0].Rank != 1 {
			t.Errorf("expected rank 1, got %d", result.Candidates[0].Rank)
		}
	}
	t.Logf("Real DB Owner Yield Ranking: %d companies, %d beating hurdle (%.2f%%)",
		result.Total, result.BeatsCount, result.HurdleRate*100)
}
