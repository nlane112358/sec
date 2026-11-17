package commands

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sec/internal/store"
)

func TestRunPrecomputeCommand(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "sec.db")
	s, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	cik := "0000009001"
	if err := s.SaveCompany(ctx, cik, "CLI Test Corp", "CLIT", "7372", "Software"); err != nil {
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
			{"OperatingIncomeLoss", "USD", start, 100_000_000},
			{"NetIncomeLoss", "USD", start, 75_000_000},
			{"NetCashProvidedByUsedInOperatingActivities", "USD", start, 110_000_000},
			{"ResearchAndDevelopmentExpense", "USD", start, 50_000_000},
			{"WeightedAverageNumberOfDilutedSharesOutstanding", "shares", start, 50_000_000},
			{"StockholdersEquity", "USD", "", 300_000_000},
		} {
			facts = append(facts, store.FinancialFact{
				Company: "CLI Test Corp", CIK: cik, Ticker: "CLIT", Taxonomy: "us-gaap",
				Concept: item.c, Label: item.c, Unit: item.u, Value: item.v,
				Start: item.s, End: end, FilingDate: fdate, Form: "10-K", Accession: acc,
			})
		}
	}
	if err := s.SaveFacts(ctx, "CLIT", facts); err != nil {
		t.Fatalf("save facts: %v", err)
	}
	if err := s.SaveFilings(ctx, filings); err != nil {
		t.Fatalf("save filings: %v", err)
	}
	_ = s.Close()

	var stdout, stderr bytes.Buffer
	if err := RunPrecompute(ctx, []string{"--data-dir", dataDir, "--workers", "2"}, &stdout, &stderr); err != nil {
		t.Fatalf("RunPrecompute failed: %v\nstderr=%s", err, stderr.String())
	}
	outStr := stdout.String()
	if !strings.Contains(outStr, "precomputed=1") || !strings.Contains(outStr, "clean_summaries=1") || !strings.Contains(outStr, "rank_6y=1") || !strings.Contains(outStr, "gaap_arb_5y=1") {
		t.Fatalf("unexpected precompute output: %q", outStr)
	}

	// Second run without --force should report precomputed=0.
	stdout.Reset()
	stderr.Reset()
	if err := RunPrecompute(ctx, []string{"--data-dir", dataDir}, &stdout, &stderr); err != nil {
		t.Fatalf("second RunPrecompute failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "precomputed=0") {
		t.Fatalf("expected second run precomputed=0, got %q", stdout.String())
	}
}
