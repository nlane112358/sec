package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sec/internal/edgar"
	"sec/internal/market"
	"sec/internal/store"
)

type mockMarketEnv struct {
	dataDir      string
	universesDir string
	edgarCalls   atomic.Int64
	quoteCalls   atomic.Int64
	fxCalls      atomic.Int64
	cleanup      func()
}

func setupMockEnv(t *testing.T) *mockMarketEnv {
	t.Helper()
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	uDir := filepath.Join(tmp, "universes")
	cacheDir := filepath.Join(tmp, "cache")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(uDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEC_UNIVERSES_DIR", uDir)
	t.Setenv("SEC_CACHE_DIR", cacheDir)

	// Seed standard universe files in temp universes dir.
	_ = os.WriteFile(filepath.Join(uDir, "sp500.txt"), []byte("AAPL\nMSFT\nBADTICKER\n"), 0644)
	_ = os.WriteFile(filepath.Join(uDir, "euro_uk_compounders_20f.txt"), []byte("ASML\n"), 0644)

	env := &mockMarketEnv{dataDir: dataDir, universesDir: uDir}

	nowYr := time.Now().Year()
	y0 := nowYr - 1
	recentFiling := time.Now().AddDate(0, -1, 0).Format("2006-01-02")

	buildCompanyFacts := func(cikInt int, entity, tax, unit, form string, revBase, opMargin, rdRatio, equityBase, shares float64) map[string]any {
		buildSeries := func(valFn func(yrIdx int) float64, instant bool) []map[string]any {
			var entries []map[string]any
			for i := 0; i < 6; i++ {
				yr := y0 - i
				acc := fmt.Sprintf("%010d-%02d-000001", cikInt, yr%100)
				filed := recentFiling
				if i > 0 {
					filed = fmt.Sprintf("%d-02-15", yr+1)
				}
				entry := map[string]any{
					"accn":  acc,
					"end":   fmt.Sprintf("%d-12-31", yr),
					"form":  form,
					"filed": filed,
					"val":   valFn(i),
				}
				if !instant {
					entry["start"] = fmt.Sprintf("%d-01-01", yr)
				}
				entries = append(entries, entry)
			}
			return entries
		}

		// Also add quarterly 10-Q entries for AAPL so quarterly/TTM views are richly exercised.
		addQuarterly := func(entries []map[string]any, qVal float64) []map[string]any {
			if form != "10-K" {
				return entries
			}
			qEnds := []struct{ s, e, f string }{
				{fmt.Sprintf("%d-07-01", nowYr), fmt.Sprintf("%d-09-30", nowYr), recentFiling},
				{fmt.Sprintf("%d-04-01", nowYr), fmt.Sprintf("%d-06-30", nowYr), fmt.Sprintf("%d-08-01", nowYr)},
				{fmt.Sprintf("%d-01-01", nowYr), fmt.Sprintf("%d-03-31", nowYr), fmt.Sprintf("%d-05-01", nowYr)},
				{fmt.Sprintf("%d-10-01", y0), fmt.Sprintf("%d-12-31", y0), fmt.Sprintf("%d-02-01", nowYr)},
				{fmt.Sprintf("%d-07-01", y0), fmt.Sprintf("%d-09-30", y0), fmt.Sprintf("%d-11-01", y0)},
			}
			for idx, q := range qEnds {
				entries = append(entries, map[string]any{
					"accn":  fmt.Sprintf("%010d-%02d-10000%d", cikInt, nowYr%100, idx+1),
					"start": q.s,
					"end":   q.e,
					"form":  "10-Q",
					"filed": q.f,
					"val":   qVal * (1.0 + float64(5-idx)*0.03),
				})
			}
			return entries
		}

		revEntries := addQuarterly(buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) }, false), revBase*0.30)
		opEntries := addQuarterly(buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) * opMargin }, false), revBase*0.30*opMargin)
		niEntries := addQuarterly(buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) * opMargin * 0.78 }, false), revBase*0.30*opMargin*0.78)
		ocfEntries := addQuarterly(buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) * (opMargin + 0.04) }, false), revBase*0.30*(opMargin+0.04))
		capexEntries := addQuarterly(buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) * 0.04 }, false), revBase*0.30*0.04)
		rdEntries := addQuarterly(buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) * rdRatio }, false), revBase*0.30*rdRatio)
		sgaEntries := addQuarterly(buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) * 0.10 }, false), revBase*0.30*0.10)
		sbcEntries := addQuarterly(buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) * 0.015 }, false), revBase*0.30*0.015)
		daEntries := addQuarterly(buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) * 0.035 }, false), revBase*0.30*0.035)
		taxEntries := buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) * opMargin * 0.20 }, false)
		pretaxEntries := buildSeries(func(i int) float64 { return revBase * pow(1.08, 5-i) * opMargin }, false)
		cashEntries := buildSeries(func(i int) float64 { return equityBase * 0.35 }, true)
		debtEntries := buildSeries(func(i int) float64 { return equityBase * 0.25 }, true)
		caEntries := buildSeries(func(i int) float64 { return equityBase * 0.90 }, true)
		clEntries := buildSeries(func(i int) float64 { return equityBase * 0.55 }, true)
		assetsEntries := buildSeries(func(i int) float64 { return equityBase * 2.0 }, true)
		eqEntries := buildSeries(func(i int) float64 { return equityBase }, true)
		shareEntries := buildSeries(func(i int) float64 { return shares * pow(1.02, i) }, false)

		revTag, opTag, niTag, ocfTag, capexTag, rdTag, sgaTag, sbcTag, daTag, taxTag, pretaxTag, cashTag, debtTag, caTag, clTag, assetsTag, eqTag, shareTag :=
			"Revenues", "OperatingIncomeLoss", "NetIncomeLoss", "NetCashProvidedByUsedInOperatingActivities",
			"PaymentsToAcquirePropertyPlantAndEquipment", "ResearchAndDevelopmentExpense", "SellingGeneralAndAdministrativeExpense",
			"ShareBasedCompensation", "DepreciationDepletionAndAmortization", "IncomeTaxExpenseBenefit",
			"IncomeLossFromContinuingOperationsBeforeIncomeTaxesExtraordinaryItemsNoncontrollingInterest",
			"CashAndCashEquivalentsAtCarryingValue", "LongTermDebtNoncurrent", "AssetsCurrent", "LiabilitiesCurrent",
			"Assets", "StockholdersEquity", "WeightedAverageNumberOfDilutedSharesOutstanding"
		if tax == "ifrs-full" {
			revTag, opTag, niTag, ocfTag, capexTag, rdTag, sgaTag, daTag, taxTag, pretaxTag, cashTag, debtTag, caTag, clTag, assetsTag, eqTag, shareTag =
				"Revenue", "ProfitLossFromOperatingActivities", "ProfitLoss", "CashFlowsFromUsedInOperatingActivities",
				"PurchaseOfPropertyPlantAndEquipment", "ResearchAndDevelopmentExpense", "SellingGeneralAndAdministrativeExpense",
				"DepreciationAndAmortisationExpense", "IncomeTaxExpenseContinuingOperations", "ProfitLossBeforeTax",
				"CashAndCashEquivalents", "Borrowings", "CurrentAssets", "CurrentLiabilities",
				"Assets", "Equity", "DilutedWeightedAverageNumberOfOrdinarySharesOutstanding"
		}

		return map[string]any{
			"cik":        cikInt,
			"entityName": entity,
			"facts": map[string]any{
				tax: map[string]any{
					revTag:    map[string]any{"units": map[string]any{unit: revEntries}},
					opTag:     map[string]any{"units": map[string]any{unit: opEntries}},
					niTag:     map[string]any{"units": map[string]any{unit: niEntries}},
					ocfTag:    map[string]any{"units": map[string]any{unit: ocfEntries}},
					capexTag:  map[string]any{"units": map[string]any{unit: capexEntries}},
					rdTag:     map[string]any{"units": map[string]any{unit: rdEntries}},
					sgaTag:    map[string]any{"units": map[string]any{unit: sgaEntries}},
					sbcTag:    map[string]any{"units": map[string]any{unit: sbcEntries}},
					daTag:     map[string]any{"units": map[string]any{unit: daEntries}},
					taxTag:    map[string]any{"units": map[string]any{unit: taxEntries}},
					pretaxTag: map[string]any{"units": map[string]any{unit: pretaxEntries}},
					cashTag:   map[string]any{"units": map[string]any{unit: cashEntries}},
					debtTag:   map[string]any{"units": map[string]any{unit: debtEntries}},
					caTag:     map[string]any{"units": map[string]any{unit: caEntries}},
					clTag:     map[string]any{"units": map[string]any{unit: clEntries}},
					assetsTag: map[string]any{"units": map[string]any{unit: assetsEntries}},
					eqTag:     map[string]any{"units": map[string]any{unit: eqEntries}},
					shareTag:  map[string]any{"units": map[string]any{"shares": shareEntries}},
				},
			},
		}
	}

	buildSubmissions := func(cikStr, entity, sic, sicDesc, form string) map[string]any {
		acc0 := fmt.Sprintf("%s-%02d-000001", cikStr, y0%100)
		acc1 := fmt.Sprintf("%s-%02d-000001", cikStr, (y0-1)%100)
		forms := []string{form, form}
		accs := []string{acc0, acc1}
		fdates := []string{recentFiling, fmt.Sprintf("%d-02-15", y0)}
		rdates := []string{fmt.Sprintf("%d-12-31", y0), fmt.Sprintf("%d-12-31", y0-1)}
		docs := []string{"filing-curr.htm", "filing-prior.htm"}
		titles := []string{form + " Current", form + " Prior"}
		if form == "10-K" {
			forms = append([]string{"10-Q"}, forms...)
			accs = append([]string{fmt.Sprintf("%s-%02d-100001", cikStr, nowYr%100)}, accs...)
			fdates = append([]string{recentFiling}, fdates...)
			rdates = append([]string{fmt.Sprintf("%d-09-30", nowYr)}, rdates...)
			docs = append([]string{"q3.htm"}, docs...)
			titles = append([]string{"Form 10-Q Q3"}, titles...)
		}
		return map[string]any{
			"cik":            cikStr,
			"name":           entity,
			"sic":            sic,
			"sicDescription": sicDesc,
			"filings": map[string]any{
				"recent": map[string]any{
					"accessionNumber":       accs,
					"filingDate":            fdates,
					"reportDate":            rdates,
					"form":                  forms,
					"primaryDocument":       docs,
					"primaryDocDescription": titles,
				},
			},
		}
	}

	edgarHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.edgarCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/files/company_tickers.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"0": map[string]any{"cik_str": 320193, "ticker": "AAPL", "title": "Apple Inc."},
				"1": map[string]any{"cik_str": 789019, "ticker": "MSFT", "title": "Microsoft Corp"},
				"2": map[string]any{"cik_str": 937966, "ticker": "ASML", "title": "ASML Holding N.V."},
			})
		case r.URL.Path == "/submissions/CIK0000320193.json":
			_ = json.NewEncoder(w).Encode(buildSubmissions("0000320193", "Apple Inc.", "3571", "Electronic Computers", "10-K"))
		case r.URL.Path == "/submissions/CIK0000789019.json":
			_ = json.NewEncoder(w).Encode(buildSubmissions("0000789019", "Microsoft Corp", "7372", "Prepackaged Software", "10-K"))
		case r.URL.Path == "/submissions/CIK0000937966.json":
			_ = json.NewEncoder(w).Encode(buildSubmissions("0000937966", "ASML Holding N.V.", "3559", "Special Industry Machinery", "20-F"))
		case r.URL.Path == "/api/xbrl/companyfacts/CIK0000320193.json":
			_ = json.NewEncoder(w).Encode(buildCompanyFacts(320193, "Apple Inc.", "us-gaap", "USD", "10-K", 300e9, 0.30, 0.08, 70e9, 15e9))
		case r.URL.Path == "/api/xbrl/companyfacts/CIK0000789019.json":
			_ = json.NewEncoder(w).Encode(buildCompanyFacts(789019, "Microsoft Corp", "us-gaap", "USD", "10-K", 180e9, 0.42, 0.12, 200e9, 7.5e9))
		case r.URL.Path == "/api/xbrl/companyfacts/CIK0000937966.json":
			_ = json.NewEncoder(w).Encode(buildCompanyFacts(937966, "ASML Holding N.V.", "ifrs-full", "EUR", "20-F", 22e9, 0.32, 0.14, 15e9, 400e6))
		case strings.HasSuffix(r.URL.Path, "filing-curr.htm"):
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body><h2>Item 1A. Risk Factors</h2><p>Global competition remains intense.</p><p>New artificial intelligence export restrictions could reduce revenue.</p><h2>Item 1B. Unresolved Staff Comments</h2><h2>Item 7. Management's Discussion and Analysis</h2><p>Operating margin expanded due to services growth.</p><h2>Item 7A. Quantitative</h2></body></html>`))
		case strings.HasSuffix(r.URL.Path, "filing-prior.htm"):
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body><h2>Item 1A. Risk Factors</h2><p>Global competition remains intense.</p><p>Legacy supply chain risks could reduce revenue.</p><h2>Item 1B. Unresolved Staff Comments</h2><h2>Item 7. Management's Discussion and Analysis</h2><p>Operating margin expanded due to hardware growth.</p><h2>Item 7A. Quantitative</h2></body></html>`))
		default:
			http.NotFound(w, r)
		}
	})

	marketHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "EURUSD=X") {
			env.fxCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"chart": map[string]any{
					"result": []any{map[string]any{"meta": map[string]any{"currency": "USD", "regularMarketPrice": 1.095}}},
				},
			})
			return
		}
		env.quoteCalls.Add(1)
		price := 200.0
		switch {
		case strings.HasSuffix(r.URL.Path, "/AAPL"):
			price = 210.0
		case strings.HasSuffix(r.URL.Path, "/MSFT"):
			price = 420.0
		case strings.HasSuffix(r.URL.Path, "/ASML"):
			price = 800.0
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"chart": map[string]any{
				"result": []any{map[string]any{"meta": map[string]any{
					"currency":           "USD",
					"regularMarketPrice": price,
					"fiftyTwoWeekHigh":   price * 1.15,
					"fiftyTwoWeekLow":    price * 0.80,
				}}},
			},
		})
	})

	edgarRT := roundTripHandler(edgarHandler)
	marketRT := roundTripHandler(marketHandler)

	restoreEDGAR := SetEDGARClientFactoryForTesting(func(ua string) *edgar.Client {
		c := edgar.NewClientWithURLs(ua, "http://edgar.local", "http://edgar.local/files/company_tickers.json", "http://edgar.local/Archives/edgar/data")
		c.SetTransport(edgarRT)
		return c
	})
	restoreMarketURL := market.SetMarketEndpointsForTesting("http://market.local", "http://market.local")
	restoreMarketRT := market.SetMarketTransportForTesting(marketRT)

	env.cleanup = func() {
		restoreEDGAR()
		restoreMarketRT()
		restoreMarketURL()
	}
	t.Cleanup(env.cleanup)
	return env
}

type roundTripHandler http.HandlerFunc

func (fn roundTripHandler) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec.Result(), nil
}

func pow(base float64, exp int) float64 {
	res := 1.0
	for i := 0; i < exp; i++ {
		res *= base
	}
	return res
}

func TestJITHydrationASMLAndFreshnessCache(t *testing.T) {
	env := setupMockEnv(t)
	ctx := context.Background()

	// 1. First run: ASML not in DB yet -> triggers JIT EDGAR + EUR/USD FX + live quote + spinner!
	var out1, err1 bytes.Buffer
	if err := RunInspect(ctx, []string{"ASML", "--data-dir", env.dataDir}, &out1, &err1); err != nil {
		t.Fatalf("RunInspect ASML failed: %v\nstderr=%s", err, err1.String())
	}
	stderr1 := err1.String()
	stdout1 := out1.String()
	if !strings.Contains(stderr1, "Fetching 20-F facts (EDGAR)...") || !strings.Contains(stderr1, "Applying EUR/USD FX...") || !strings.Contains(stderr1, "Done in") {
		t.Fatalf("expected inline spinner steps on stderr, got:\n%s", stderr1)
	}
	if !strings.Contains(stdout1, "Data:") || !strings.Contains(stdout1, "20-F") || !strings.Contains(stdout1, "EUR→USD 1.0950 (live)") || !strings.Contains(stdout1, "Price $800.00 (live)") || !strings.Contains(stdout1, "ADR 1:1 (ASML.AS)") {
		t.Fatalf("expected freshness metadata header on stdout, got:\n%s", stdout1)
	}
	if !strings.Contains(stdout1, "ASML Holding N.V. (ASML)") {
		t.Fatalf("expected ASML financial summary table, got:\n%s", stdout1)
	}

	edgarBefore := env.edgarCalls.Load()
	quoteBefore := env.quoteCalls.Load()

	// 2. Second run: pure cache hit -> 0 network calls, 0 spinner output!
	var out2, err2 bytes.Buffer
	if err := RunInspect(ctx, []string{"ASML", "--data-dir", env.dataDir}, &out2, &err2); err != nil {
		t.Fatalf("second RunInspect ASML failed: %v", err)
	}
	if env.edgarCalls.Load() != edgarBefore || env.quoteCalls.Load() != quoteBefore {
		t.Fatalf("expected second run to hit SQLite cache (edgar %d->%d, quote %d->%d)",
			edgarBefore, env.edgarCalls.Load(), quoteBefore, env.quoteCalls.Load())
	}
	if err2.Len() != 0 {
		t.Fatalf("expected clean stderr on cache hit, got: %q", err2.String())
	}
	if !strings.Contains(out2.String(), "Price $800.00 (synced") || !strings.Contains(out2.String(), "EUR→USD 1.0950") {
		t.Fatalf("expected cached freshness indicators, got:\n%s", out2.String())
	}

	// 3. Explicit --refresh forces fresh pulls.
	var out3, err3 bytes.Buffer
	if err := RunInspect(ctx, []string{"ASML", "--refresh", "--data-dir", env.dataDir}, &out3, &err3); err != nil {
		t.Fatalf("--refresh RunInspect ASML failed: %v", err)
	}
	if env.edgarCalls.Load() <= edgarBefore || env.quoteCalls.Load() <= quoteBefore {
		t.Fatal("expected --refresh to pull fresh EDGAR and quote data")
	}

	// 4. Offline missing ticker fails gracefully without network calls.
	var out4, err4 bytes.Buffer
	err := RunInspect(ctx, []string{"AAPL", "--offline", "--data-dir", env.dataDir}, &out4, &err4)
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("expected offline error for uncached ticker, got: %v", err)
	}
}

func TestInspectSubViewsAndValueCommands(t *testing.T) {
	env := setupMockEnv(t)
	ctx := context.Background()

	// Quarterly view on AAPL (hydrates AAPL automatically)
	var outQ, errQ bytes.Buffer
	if err := RunInspect(ctx, []string{"AAPL", "quarterly", "--quarters", "4", "--data-dir", env.dataDir}, &outQ, &errQ); err != nil {
		t.Fatalf("inspect quarterly failed: %v", err)
	}
	if !strings.Contains(outQ.String(), "quarterly facts (10-Q)") || !strings.Contains(outQ.String(), "Data: Q3 ") {
		t.Fatalf("unexpected quarterly output:\n%s", outQ.String())
	}

	// TTM view with inflections
	var outTTM, errTTM bytes.Buffer
	if err := RunInspect(ctx, []string{"AAPL", "ttm", "--data-dir", env.dataDir}, &outTTM, &errTTM); err != nil {
		t.Fatalf("inspect ttm failed: %v", err)
	}
	if !strings.Contains(outTTM.String(), "TTM & multi-year facts") || !strings.Contains(outTTM.String(), "Quarterly Inflections") {
		t.Fatalf("unexpected ttm output:\n%s", outTTM.String())
	}

	// Filing diff (Item 1A & Item 7)
	var outDiff, errDiff bytes.Buffer
	if err := RunInspect(ctx, []string{"AAPL", "diff", "--item", "1A", "--data-dir", env.dataDir}, &outDiff, &errDiff); err != nil {
		t.Fatalf("inspect diff failed: %v", err)
	}
	if !strings.Contains(outDiff.String(), "Item 1A. Risk Factors Diff") ||
		!strings.Contains(outDiff.String(), "+ New artificial intelligence export restrictions could reduce revenue.") ||
		!strings.Contains(outDiff.String(), "- Legacy supply chain risks could reduce revenue.") {
		t.Fatalf("unexpected diff output:\n%s", outDiff.String())
	}

	var outDiff7, errDiff7 bytes.Buffer
	if err := RunInspect(ctx, []string{"AAPL", "diff", "--item", "7", "--data-dir", env.dataDir}, &outDiff7, &errDiff7); err != nil {
		t.Fatalf("inspect diff Item 7 failed: %v", err)
	}
	if !strings.Contains(outDiff7.String(), "Item 7. Management's Discussion and Analysis Diff") || !strings.Contains(outDiff7.String(), "{+services+}") {
		t.Fatalf("unexpected diff Item 7 output:\n%s", outDiff7.String())
	}

	// Audit view
	var outAudit, errAudit bytes.Buffer
	if err := RunInspect(ctx, []string{"AAPL", "audit", "--data-dir", env.dataDir}, &outAudit, &errAudit); err != nil {
		t.Fatalf("inspect audit failed: %v", err)
	}
	if !strings.Contains(outAudit.String(), "Diagnostic Audit for Apple Inc. (AAPL)") {
		t.Fatalf("unexpected audit output:\n%s", outAudit.String())
	}

	// Filings list view (non-blocking)
	var outFilings, errFilings bytes.Buffer
	if err := RunInspect(ctx, []string{"AAPL", "filings", "--list", "--form", "10-K", "--data-dir", env.dataDir}, &outFilings, &errFilings); err != nil {
		t.Fatalf("inspect filings failed: %v", err)
	}
	if !strings.Contains(outFilings.String(), "10-K") || !strings.Contains(outFilings.String(), "10-K Current") {
		t.Fatalf("unexpected filings output:\n%s", outFilings.String())
	}

	// Value subcommands: dcf, reverse-dcf, wacc (using ASML in EUR with live quote & FX!)
	var outDCF, errDCF bytes.Buffer
	if err := RunValue(ctx, []string{"ASML", "dcf", "--data-dir", env.dataDir}, &outDCF, &errDCF); err != nil {
		t.Fatalf("value dcf ASML failed: %v", err)
	}
	if !strings.Contains(outDCF.String(), "Intrinsic Value / Share:") || !strings.Contains(outDCF.String(), "Market Comparison:") {
		t.Fatalf("unexpected DCF output:\n%s", outDCF.String())
	}

	var outRev, errRev bytes.Buffer
	if err := RunValue(ctx, []string{"ASML", "reverse-dcf", "--data-dir", env.dataDir}, &outRev, &errRev); err != nil {
		t.Fatalf("value reverse-dcf ASML failed: %v", err)
	}
	if !strings.Contains(outRev.String(), "Market-Implied 5Y CAGR:") {
		t.Fatalf("unexpected reverse-DCF output:\n%s", outRev.String())
	}

	var outWACC, errWACC bytes.Buffer
	if err := RunValue(ctx, []string{"ASML", "wacc", "--beta", "1.2", "--data-dir", env.dataDir}, &outWACC, &errWACC); err != nil {
		t.Fatalf("value wacc ASML failed: %v", err)
	}
	if !strings.Contains(outWACC.String(), "WACC Estimate:") || !strings.Contains(outWACC.String(), "Cost of Equity (Ke):") {
		t.Fatalf("unexpected WACC output:\n%s", outWACC.String())
	}
}

func TestBatchProgressScreenStrategiesAndFormulaWhere(t *testing.T) {
	env := setupMockEnv(t)
	ctx := context.Background()

	// Pre-hydrate AAPL so @sp500 (AAPL, MSFT, BADTICKER) exercises cache hit + EDGAR fetch + non-fatal missing ticker!
	var tmpOut, tmpErr bytes.Buffer
	if err := RunInspect(ctx, []string{"AAPL", "--data-dir", env.dataDir}, &tmpOut, &tmpErr); err != nil {
		t.Fatalf("seed AAPL failed: %v", err)
	}

	// Formula screen: @sp500 --where "roic > 0.15 && fcf_yield > 0.03"
	var outWhere, errWhere bytes.Buffer
	if err := RunScreen(ctx, []string{"@sp500", "--where", "roic > 0.15 && fcf_yield > 0.03", "--data-dir", env.dataDir}, &outWhere, &errWhere); err != nil {
		t.Fatalf("formula screen failed: %v\nstderr=%s", err, errWhere.String())
	}
	if !strings.Contains(errWhere.String(), "cache: 1") || !strings.Contains(errWhere.String(), "fetched: 1") || !strings.Contains(errWhere.String(), "failed: 1") {
		t.Fatalf("expected batch progress bar with cache=1 fetched=1 failed=1, got:\n%s", errWhere.String())
	}
	if !strings.Contains(outWhere.String(), "Universe: @sp500") || !strings.Contains(outWhere.String(), "AAPL") || !strings.Contains(outWhere.String(), "MSFT") {
		t.Fatalf("unexpected formula screen output:\n%s", outWhere.String())
	}

	// All 5 strategy screens on @sp500 (now cached!)
	strategies := []struct {
		args []string
		want string
	}{
		{[]string{"rank", "@sp500", "--data-dir", env.dataDir}, "Fundamental ranks:"},
		{[]string{"owner-yield", "@sp500", "--min-yield", "1", "--data-dir", env.dataDir}, "Owner Earnings Yield Screener"},
		{[]string{"quality-discount", "@sp500", "--min-score", "10", "--min-roic", "5", "--min-discount", "-500", "--data-dir", env.dataDir}, "Quality-at-a-Discount (QaaD) Screener"},
		{[]string{"arbitrage", "@sp500", "--min-score", "10", "--min-roic", "5", "--require-buybacks=false", "--data-dir", env.dataDir}, "Expectations Arbitrage"},
		{[]string{"gaap-arbitrage", "@sp500", "--min-adj-roic", "5", "--min-yield", "1", "--data-dir", env.dataDir}, "Modern GAAP Arbitrage"},
	}
	for _, tc := range strategies {
		var sOut, sErr bytes.Buffer
		if err := RunScreen(ctx, tc.args, &sOut, &sErr); err != nil {
			t.Fatalf("screen %v failed: %v\nstderr=%s", tc.args, err, sErr.String())
		}
		if !strings.Contains(sOut.String(), tc.want) {
			t.Fatalf("screen %v output missing %q:\n%s", tc.args, tc.want, sOut.String())
		}
	}
}

func TestUniverseDataPruneAndShellCompletion(t *testing.T) {
	env := setupMockEnv(t)
	ctx := context.Background()

	// Seed ASML via inspect
	var tmpOut, tmpErr bytes.Buffer
	if err := RunInspect(ctx, []string{"ASML", "--data-dir", env.dataDir}, &tmpOut, &tmpErr); err != nil {
		t.Fatal(err)
	}

	// Create custom @my-picks universe
	var uCreateOut, uCreateErr bytes.Buffer
	if err := RunUniverse(ctx, []string{"create", "@my-picks", "ASML", "AAPL", "--description", "Top Picks"}, &uCreateOut, &uCreateErr); err != nil {
		t.Fatalf("universe create failed: %v", err)
	}
	if !strings.Contains(uCreateOut.String(), "@my-picks") {
		t.Fatalf("unexpected universe create output: %s", uCreateOut.String())
	}

	// Show @my-picks (ASML cached, AAPL pending)
	var uShowOut, uShowErr bytes.Buffer
	if err := RunUniverse(ctx, []string{"show", "@my-picks", "--data-dir", env.dataDir}, &uShowOut, &uShowErr); err != nil {
		t.Fatalf("universe show failed: %v", err)
	}
	if !strings.Contains(uShowOut.String(), "cached") || !strings.Contains(uShowOut.String(), "pending (JIT)") {
		t.Fatalf("unexpected universe show output:\n%s", uShowOut.String())
	}

	// Data status --list
	var stOut, stErr bytes.Buffer
	if err := RunData(ctx, []string{"status", "--list", "--data-dir", env.dataDir}, &stOut, &stErr); err != nil {
		t.Fatalf("data status failed: %v", err)
	}
	if !strings.Contains(stOut.String(), "1 hydrated") || !strings.Contains(stOut.String(), "ASML") {
		t.Fatalf("unexpected data status output:\n%s", stOut.String())
	}

	// Data export JSON
	var expOut, expErr bytes.Buffer
	if err := RunData(ctx, []string{"export", "ASML", "--format", "json", "--offline", "--data-dir", env.dataDir}, &expOut, &expErr); err != nil {
		t.Fatalf("data export failed: %v", err)
	}
	if !strings.Contains(expOut.String(), `"ticker": "ASML"`) {
		t.Fatalf("unexpected export output:\n%s", expOut.String())
	}

	// Data prune --all
	var prOut, prErr bytes.Buffer
	if err := RunData(ctx, []string{"prune", "--all", "--data-dir", env.dataDir}, &prOut, &prErr); err != nil {
		t.Fatalf("data prune failed: %v", err)
	}
	if !strings.Contains(prOut.String(), "quotes=1") || !strings.Contains(prOut.String(), "fx_rates=1") {
		t.Fatalf("unexpected prune output: %s", prOut.String())
	}

	// Completion scripts & dynamic __complete
	for _, sh := range []string{"zsh", "bash", "fish"} {
		var cOut, cErr bytes.Buffer
		if err := RunCompletion(ctx, []string{sh}, &cOut, &cErr); err != nil {
			t.Fatalf("completion %s failed: %v", sh, err)
		}
		if !strings.Contains(cOut.String(), "__complete") {
			t.Fatalf("completion %s missing __complete hook", sh)
		}
	}

	var compOut, compErr bytes.Buffer
	if err := RunHiddenComplete(ctx, []string{"--data-dir", env.dataDir, "screen", "@"}, &compOut, &compErr); err != nil {
		t.Fatalf("__complete failed: %v", err)
	}
	if !strings.Contains(compOut.String(), "@euro-uk") || !strings.Contains(compOut.String(), "@my-picks") || !strings.Contains(compOut.String(), "@sp500") {
		t.Fatalf("expected @universe aliases in completion output, got:\n%s", compOut.String())
	}

	var inspCompOut bytes.Buffer
	if err := RunHiddenComplete(ctx, []string{"--data-dir", env.dataDir, "inspect", "ASML", ""}, &inspCompOut, &compErr); err != nil {
		t.Fatalf("__complete inspect failed: %v", err)
	}
	if !strings.Contains(inspCompOut.String(), "quarterly") || !strings.Contains(inspCompOut.String(), "diff") {
		t.Fatalf("expected inspect subviews in completion, got:\n%s", inspCompOut.String())
	}

	// Ensure unused store import check passes if needed
	_ = store.DefaultDirectory()
}
