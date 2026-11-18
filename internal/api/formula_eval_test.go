package api

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sec/internal/screener"
	"sec/internal/store"
)

func TestParseAndEvalFormula(t *testing.T) {
	expr, err := ParseFormula(`roic > 0.15 && fcf_yield > 5% and market_cap >= 1B and sector != "Financials"`)
	if err != nil {
		t.Fatalf("ParseFormula failed: %v", err)
	}
	if !expr.NeedsQuotes() {
		t.Fatal("expected NeedsQuotes() to be true")
	}

	pureExpr, err := ParseFormula(`roic > 15% && revenue > 100M`)
	if err != nil {
		t.Fatalf("ParseFormula pure failed: %v", err)
	}
	if pureExpr.NeedsQuotes() {
		t.Fatal("expected pure fundamental formula NeedsQuotes() to be false")
	}

	// Row failing roic > 0.15 should short-circuit to matched=false, unknown=false even without quotes!
	lowROIC := &FormulaCompanyRow{
		Ticker: "LOW", Sector: "Nonfinancial", Currency: "USD",
		nums: map[string]float64{"ROIC": 0.10, "FCF": 100e6, "SHARES": 10e6},
	}
	matched, unknown := expr.Matches(lowROIC, false)
	if matched || unknown {
		t.Fatalf("expected low-ROIC row to short-circuit false without quotes, got matched=%v unknown=%v", matched, unknown)
	}

	// Row passing roic > 0.15 without quotes should return unknown=true so caller knows to fetch quote.
	highROIC := &FormulaCompanyRow{
		Ticker: "HIGH", Sector: "Nonfinancial", Currency: "USD",
		nums: map[string]float64{"ROIC": 0.25, "FCF": 100e6, "SHARES": 50e6},
	}
	matched, unknown = expr.Matches(highROIC, false)
	if matched || !unknown {
		t.Fatalf("expected high-ROIC row without quotes to be unknown=true, got matched=%v unknown=%v", matched, unknown)
	}

	// With price = $30 -> mcap = $1.5B, fcf_yield = 100M / 1.5B = 6.67% > 5% -> matches!
	highROIC.Price = 30.0
	highROIC.HasPrice = true
	matched, unknown = expr.Matches(highROIC, true)
	if !matched || unknown {
		t.Fatalf("expected high-ROIC row with $30 price to match, got matched=%v unknown=%v", matched, unknown)
	}

	// Arithmetic + helper functions
	arith, err := ParseFormula(`(fcf - coalesce(sbc, 0)) / market_cap > 0.05 && abs(-5) == 5`)
	if err != nil {
		t.Fatalf("ParseFormula arithmetic failed: %v", err)
	}
	highROIC.nums["SBC"] = 10e6
	matched, unknown = arith.Matches(highROIC, true)
	if !matched || unknown {
		t.Fatalf("expected arithmetic formula to match (90M/1.5B = 6%% > 5%%), got matched=%v unknown=%v", matched, unknown)
	}
}

func TestScreenByFormulaAndLazyQuotes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sec.db")
	s, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer s.Close()

	seedCompany := func(cik, ticker, name string, rev, opInc, ocf, capex, equity float64) {
		if err := s.SaveCompany(ctx, cik, name, ticker, "7372", "Software"); err != nil {
			t.Fatalf("save company: %v", err)
		}
		nowYr := time.Now().Year()
		recent := time.Now().AddDate(0, -1, 0).Format("2006-01-02")
		var facts []store.FinancialFact
		var filings []store.Filing
		for i := 0; i < 6; i++ {
			yr := nowYr - 1 - i
			acc := fmt.Sprintf("%s-%02d-000001", cik, yr%100)
			start := fmt.Sprintf("%d-01-01", yr)
			end := fmt.Sprintf("%d-12-31", yr)
			fdate := recent
			if i > 0 {
				fdate = fmt.Sprintf("%d-02-15", yr+1)
			}
			filings = append(filings, store.Filing{
				CIK: cik, Accession: acc, Form: "10-K", FilingDate: fdate, ReportDate: end, PrimaryDocument: "k.htm",
			})
			for _, item := range []struct {
				c, u, st string
				v        float64
			}{
				{"Revenues", "USD", start, rev},
				{"OperatingIncomeLoss", "USD", start, opInc},
				{"NetIncomeLoss", "USD", start, opInc * 0.75},
				{"NetCashProvidedByUsedInOperatingActivities", "USD", start, ocf},
				{"PaymentsToAcquirePropertyPlantAndEquipment", "USD", start, capex},
				{"CashAndCashEquivalentsAtCarryingValue", "USD", "", 100_000_000},
				{"LongTermDebtNoncurrent", "USD", "", 50_000_000},
				{"StockholdersEquity", "USD", "", equity},
				{"EntityCommonStockSharesOutstanding", "shares", "", 50_000_000},
			} {
				facts = append(facts, store.FinancialFact{
					Company: name, CIK: cik, Ticker: ticker, Taxonomy: "us-gaap",
					Concept: item.c, Label: item.c, Unit: item.u, Value: item.v,
					Start: item.st, End: end, FilingDate: fdate, Form: "10-K", Accession: acc,
				})
			}
		}
		if err := s.SaveFacts(ctx, ticker, facts); err != nil {
			t.Fatalf("save facts: %v", err)
		}
		if err := s.SaveFilings(ctx, filings); err != nil {
			t.Fatalf("save filings: %v", err)
		}
	}

	// QUAL: high ROIC (~22%), FCF = 150M. At price $40 -> mcap $2B -> fcf_yield = 7.5%
	seedCompany("0000001111", "QUAL", "Quality Co", 1_000_000_000, 220_000_000, 180_000_000, 30_000_000, 700_000_000)
	// LOWR: low ROIC (~4%), FCF = 150M. Must NOT trigger a quote fetch!
	seedCompany("0000002222", "LOWR", "Low Return Co", 1_000_000_000, 50_000_000, 180_000_000, 30_000_000, 900_000_000)

	if _, err := screener.PrecomputeMissingOrDirty(ctx, s, screener.PrecomputeOptions{RefreshRankScores: true}); err != nil {
		t.Fatalf("precompute: %v", err)
	}

	var fetchedFor []string
	res, err := ScreenByFormula(ctx, s, FormulaScreenOptions{
		Where: `roic > 0.15 && fcf_yield > 0.05`,
		OnQuoteHydrate: func(_ context.Context, missing []string) map[string]float64 {
			fetchedFor = append(fetchedFor, missing...)
			return map[string]float64{"QUAL": 40.0, "LOWR": 10.0}
		},
	})
	if err != nil {
		t.Fatalf("ScreenByFormula failed: %v", err)
	}
	if len(fetchedFor) != 1 || fetchedFor[0] != "QUAL" {
		t.Fatalf("expected quote fetch ONLY for QUAL (LOWR pruned by ROIC), got %v", fetchedFor)
	}
	if res.Matched != 1 || res.Candidates[0].Ticker != "QUAL" {
		t.Fatalf("expected 1 match QUAL, got %+v", res)
	}

	var table bytes.Buffer
	if err := WriteFormulaScreenTable(&table, res); err != nil {
		t.Fatalf("WriteFormulaScreenTable failed: %v", err)
	}
	if !strings.Contains(table.String(), "QUAL") || !strings.Contains(table.String(), "FCF_YIELD") {
		t.Fatalf("unexpected table output:\n%s", table.String())
	}
}
