package sec

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteModernGAAPTable(t *testing.T) {
	candidates := []ModernGAAPCandidate{
		{
			Ticker:                "CRM",
			Company:               "Salesforce, Inc.",
			Currency:              "USD",
			MarketCap:             280_000_000_000,
			HasMarketCap:          true,
			GAAPRevenue:           34_000_000_000,
			GAAPNetIncome:         4_100_000_000,
			GAAPOperatingIncome:   5_000_000_000,
			GAAP_PE:               68.3,
			HasGAAP_PE:            true,
			GAAPROIC:              0.082,
			HasGAAPROIC:           true,
			IntangibleCapital:     38_400_000_000,
			AdjustedEBIT:          10_200_000_000,
			AdjustedROIC:          0.264,
			HasAdjustedROIC:       true,
			ROICDelta:             0.182,
			SurgingROIC:           true,
			AdjustedOwnerEarnings: 15_500_000_000,
			OwnerEarningsYield:    0.055,
			HasOwnerEarningsYield: true,
			ArbitrageSignal:       "Severe GAAP P/E Distortion",
		},
		{
			Ticker:                "BIO",
			Company:               "Biotech Pioneer Corp",
			Currency:              "USD",
			MarketCap:             20_000_000_000,
			HasMarketCap:          true,
			GAAPRevenue:           2_000_000_000,
			GAAPNetIncome:         -200_000_000,
			GAAPOperatingIncome:   -150_000_000,
			GAAPUnprofitable:      true,
			GAAPROIC:              -0.035,
			HasGAAPROIC:           true,
			IntangibleCapital:     12_000_000_000,
			AdjustedEBIT:          2_500_000_000,
			AdjustedROIC:          0.215,
			HasAdjustedROIC:       true,
			ROICDelta:             0.250,
			SurgingROIC:           true,
			AdjustedOwnerEarnings: 1_200_000_000,
			OwnerEarningsYield:    0.060,
			HasOwnerEarningsYield: true,
			ArbitrageSignal:       "GAAP Loss → Economic Profit",
		},
	}

	var buf bytes.Buffer
	if err := WriteModernGAAPTable(&buf, candidates); err != nil {
		t.Fatalf("WriteModernGAAPTable error: %v", err)
	}

	output := buf.String()
	for _, expected := range []string{
		"RANK", "TICKER", "ADJ ROIC", "ROIC Δ", "OWNER YLD", "ARBITRAGE SIGNAL",
		"CRM", "68.3x", "26.4%", "+18.2%", "5.5%", "Severe GAAP P/E Distortion",
		"BIO", "Loss", "21.5%", "+25.0%", "6.0%", "GAAP Loss → Economic Profit",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("output missing expected substring %q:\n%s", expected, output)
		}
	}
}

func newTestStore(t *testing.T) (*Store, func()) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sec.db")
	store, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	return store, func() {
		store.Close()
	}
}

func TestModernGAAPFilterSorting(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newTestStore(t)
	defer cleanup()

	// Seed two companies:
	// Co A: Low GAAP ROIC (8%), high Adj ROIC (28%), high yield (6%)
	// Co B: Moderate GAAP ROIC (12%), moderate Adj ROIC (18%), moderate yield (4%)
	saveCompanyWithFacts(t, store, "TECH_A", "Tech Alpha Inc", "7370", "Services-Prepackaged Software", map[string]float64{
		"Revenues":                               2000,
		"OperatingIncomeLoss":                    200,
		"ResearchAndDevelopmentExpense":          600,
		"SellingGeneralAndAdministrativeExpense": 300,
		"StockholdersEquity":                     1000,
		"NetCashProvidedByUsedInOperatingActivities": 700,
		"PaymentsToAcquirePropertyPlantAndEquipment": 100,
		"CashAndCashEquivalentsAtCarryingValue":      100,
		"LongTermDebt":                              50,
		"EntityCommonStockSharesOutstanding":         100,
	})

	saveCompanyWithFacts(t, store, "TECH_B", "Tech Beta Corp", "7370", "Services-Prepackaged Software", map[string]float64{
		"Revenues":                               1500,
		"OperatingIncomeLoss":                    150,
		"ResearchAndDevelopmentExpense":          200,
		"SellingGeneralAndAdministrativeExpense": 100,
		"StockholdersEquity":                     1200,
		"NetCashProvidedByUsedInOperatingActivities": 300,
		"PaymentsToAcquirePropertyPlantAndEquipment": 50,
		"CashAndCashEquivalentsAtCarryingValue":      100,
		"LongTermDebt":                              50,
		"EntityCommonStockSharesOutstanding":         100,
	})

	marketCaps := map[string]float64{
		"TECH_A": 10000,
		"TECH_B": 8000,
	}

	candidates, err := store.ScreenModernGAAPArbitrage(ctx, []string{"TECH_A", "TECH_B"}, marketCaps, nil, ModernGAAPFilter{
		MinAdjustedROIC: 0.10,
		MinOwnerYield:   0.02,
		MinRevenue:      1000,
		Years:           3,
	})
	if err != nil {
		t.Fatalf("ScreenModernGAAPArbitrage error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	// TECH_A has higher Adjusted ROIC and higher Owner Yield, should be rank 1
	if candidates[0].Ticker != "TECH_A" {
		t.Errorf("expected TECH_A first, got %s", candidates[0].Ticker)
	}
	if candidates[0].ROICDelta <= 0 {
		t.Errorf("expected positive ROIC delta for TECH_A, got %v", candidates[0].ROICDelta)
	}
}

func TestExcludeFinancialsByDefault(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newTestStore(t)
	defer cleanup()

	// Co TECH: Nonfinancial (SIC 7372)
	saveCompanyWithFacts(t, store, "TECH_C", "Tech Gamma Inc", "7372", "Prepackaged Software", map[string]float64{
		"Revenues":                               1000,
		"OperatingIncomeLoss":                    200,
		"ResearchAndDevelopmentExpense":          300,
		"SellingGeneralAndAdministrativeExpense": 100,
		"StockholdersEquity":                     800,
		"NetCashProvidedByUsedInOperatingActivities": 400,
		"CashAndCashEquivalentsAtCarryingValue":      100,
		"EntityCommonStockSharesOutstanding":         50,
	})

	// Co FIN: Financial (SIC 6211 - Security Brokers & Dealers, e.g. IBKR)
	saveCompanyWithFacts(t, store, "FIN_B", "Financial Broker Corp", "6211", "Security Brokers", map[string]float64{
		"Revenues":                               1000,
		"OperatingIncomeLoss":                    300,
		"SellingGeneralAndAdministrativeExpense": 100,
		"StockholdersEquity":                     800,
		"NetCashProvidedByUsedInOperatingActivities": 500,
		"CashAndCashEquivalentsAtCarryingValue":      100,
		"EntityCommonStockSharesOutstanding":         50,
	})

	// 1. By default: IncludeFinancials is false -> FIN_B must be excluded
	candidates, err := store.ScreenModernGAAPArbitrage(ctx, []string{"TECH_C", "FIN_B"}, nil, nil, ModernGAAPFilter{
		MinAdjustedROIC: 0.10,
		MinRevenue:      500,
		Years:           3,
	})
	if err != nil {
		t.Fatalf("ScreenModernGAAPArbitrage error: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Ticker != "TECH_C" {
		t.Fatalf("expected only TECH_C by default (excluding SIC 6000-6799), got %d candidates: %+v", len(candidates), candidates)
	}

	// 2. When IncludeFinancials is true -> FIN_B is included
	candidatesWithFin, err := store.ScreenModernGAAPArbitrage(ctx, []string{"TECH_C", "FIN_B"}, nil, nil, ModernGAAPFilter{
		MinAdjustedROIC:   0.10,
		MinRevenue:        500,
		Years:             3,
		IncludeFinancials: true,
	})
	if err != nil {
		t.Fatalf("ScreenModernGAAPArbitrage with financials error: %v", err)
	}
	if len(candidatesWithFin) != 2 {
		t.Fatalf("expected both TECH_C and FIN_B when IncludeFinancials is true, got %d", len(candidatesWithFin))
	}
}

func TestLatestShareCountDualClass(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newTestStore(t)
	defer cleanup()

	// Seed company with dual-class structure like ERIE:
	// Class A: 46.2M shares
	// Class B: 2,542 shares
	// Both filed on same date and period
	cik := "0000922621"
	ticker := "ERIE_TEST"
	_, err := store.db.ExecContext(ctx, `INSERT INTO sec_companies (cik, company_name, requested_ticker, sic, sic_description)
VALUES (?, 'Erie Test Co', ?, '6411', 'Insurance Agents')`, cik, ticker)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.ExecContext(ctx, `INSERT INTO sec_tickers (cik, ticker) VALUES (?, ?)`, cik, ticker)
	if err != nil {
		t.Fatal(err)
	}

	// Insert Class B voting shares (2,542)
	_, err = store.db.ExecContext(ctx, `INSERT INTO sec_facts (cik, company_name, ticker, taxonomy, concept, label, unit, value, period_start, period_end, filing_date, form, accession_number)
VALUES (?, 'Erie Test Co', ?, 'dei', 'EntityCommonStockSharesOutstanding', 'Shares', 'shares', 2542, '', '2025-12-31', '2026-02-20', '10-K', 'acc-erie-01')`, cik, ticker)
	if err != nil {
		t.Fatal(err)
	}

	// Insert Class A shares (46,200,000) on same filing date and period end
	_, err = store.db.ExecContext(ctx, `INSERT INTO sec_facts (cik, company_name, ticker, taxonomy, concept, label, unit, value, period_start, period_end, filing_date, form, accession_number)
VALUES (?, 'Erie Test Co', ?, 'dei', 'EntityCommonStockSharesOutstanding', 'Shares', 'shares', 46200000, '', '2025-12-31', '2026-02-20', '10-K', 'acc-erie-02')`, cik, ticker)
	if err != nil {
		t.Fatal(err)
	}

	// Also insert diluted shares (46,200,000)
	_, err = store.db.ExecContext(ctx, `INSERT INTO sec_facts (cik, company_name, ticker, taxonomy, concept, label, unit, value, period_start, period_end, filing_date, form, accession_number)
VALUES (?, 'Erie Test Co', ?, 'us-gaap', 'WeightedAverageNumberOfDilutedSharesOutstanding', 'Diluted', 'shares', 46200000, '2025-01-01', '2025-12-31', '2026-02-20', '10-K', 'acc-erie-01')`, cik, ticker)
	if err != nil {
		t.Fatal(err)
	}

	shares, ok := store.LatestShareCount(ctx, ticker)
	if !ok {
		t.Fatal("expected LatestShareCount to find shares")
	}
	if shares != 46200000 {
		t.Fatalf("expected 46.2M shares (larger Class A value), got %v", shares)
	}
}

func saveCompanyWithFacts(t *testing.T, store *Store, ticker, name, sic, sicDesc string, annualData map[string]float64) {
	ctx := context.Background()
	cik := "000" + ticker
	if len(cik) < 10 {
		cik = strings.Repeat("0", 10-len(cik)) + cik
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO sec_companies (cik, company_name, requested_ticker, sic, sic_description)
VALUES (?, ?, ?, ?, ?) ON CONFLICT (cik) DO UPDATE SET company_name = excluded.company_name`, cik, name, ticker, sic, sicDesc)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.ExecContext(ctx, `INSERT INTO sec_tickers (cik, ticker) VALUES (?, ?) ON CONFLICT DO NOTHING`, cik, ticker)
	if err != nil {
		t.Fatal(err)
	}

	years := []string{"2025-12-31", "2024-12-31", "2023-12-31"}
	for i, periodEnd := range years {
		periodStart := "2025-01-01"
		if i == 1 {
			periodStart = "2024-01-01"
		} else if i == 2 {
			periodStart = "2023-01-01"
		}
		for concept, baseVal := range annualData {
			val := baseVal
			if i > 0 {
				val = baseVal * (1.0 - float64(i)*0.10)
			}
			isInstant := concept == "StockholdersEquity" || concept == "CashAndCashEquivalentsAtCarryingValue" || concept == "EntityCommonStockSharesOutstanding" || concept == "LongTermDebt"
			start := periodStart
			if isInstant {
				start = ""
			}
			unit := "USD"
			if concept == "EntityCommonStockSharesOutstanding" {
				unit = "shares"
			}
			_, err = store.db.ExecContext(ctx, `INSERT INTO sec_facts (cik, company_name, ticker, taxonomy, concept, label, unit, value, period_start, period_end, filing_date, form, accession_number)
VALUES (?, ?, ?, 'us-gaap', ?, ?, ?, ?, ?, ?, '2026-02-15', '10-K', ?) ON CONFLICT DO NOTHING`,
				cik, name, ticker, concept, concept, unit, val, start, periodEnd, "acc-"+ticker+periodEnd)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
