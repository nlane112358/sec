package screener

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestScreenOwnerYield(t *testing.T) {
	ctx := context.Background()
	s, cleanup := newTestStore(t)
	defer cleanup()

	// Co A: High Owner Earnings relative to market cap
	// Revenue 2000, OCF 800, CapEx 100 => FCF 700. RD 600, SGA 300 => Net Intangibles capitalized.
	saveCompanyWithFacts(t, s, "YIELD_A", "Yield Alpha Corp", "7370", "Services-Prepackaged Software", map[string]float64{
		"Revenues":                                   2000,
		"OperatingIncomeLoss":                        300,
		"ResearchAndDevelopmentExpense":              500,
		"SellingGeneralAndAdministrativeExpense":     300,
		"StockholdersEquity":                         1000,
		"NetCashProvidedByUsedInOperatingActivities": 800,
		"PaymentsToAcquirePropertyPlantAndEquipment": 100,
		"CashAndCashEquivalentsAtCarryingValue":      100,
		"LongTermDebt":                               50,
		"EntityCommonStockSharesOutstanding":         100000,
	})

	// Co B: Lower Owner Earnings relative to market cap
	saveCompanyWithFacts(t, s, "YIELD_B", "Yield Beta Inc", "7370", "Services-Prepackaged Software", map[string]float64{
		"Revenues":                                   1500,
		"OperatingIncomeLoss":                        150,
		"ResearchAndDevelopmentExpense":              100,
		"SellingGeneralAndAdministrativeExpense":     100,
		"StockholdersEquity":                         1200,
		"NetCashProvidedByUsedInOperatingActivities": 300,
		"PaymentsToAcquirePropertyPlantAndEquipment": 50,
		"CashAndCashEquivalentsAtCarryingValue":      100,
		"LongTermDebt":                               50,
		"EntityCommonStockSharesOutstanding":         100000,
	})

	prices := map[string]float64{
		"YIELD_A": 100.0,
		"YIELD_B": 200.0,
	}
	marketCaps := map[string]float64{
		"YIELD_A": 10_000_000, // 10M cap, adjusted owner earnings is high => high yield
		"YIELD_B": 20_000_000, // 20M cap, lower owner earnings => lower yield
	}

	result, err := ScreenOwnerYield(ctx, s, []string{"YIELD_A", "YIELD_B"}, marketCaps, prices, OwnerYieldFilter{
		Years:      3,
		MinRevenue: 1000,
	})
	if err != nil {
		t.Fatalf("ScreenOwnerYield error: %v", err)
	}

	if result.Total != 2 {
		t.Fatalf("expected 2 candidates, got %d", result.Total)
	}

	if result.Candidates[0].Ticker != "YIELD_A" {
		t.Errorf("expected YIELD_A to be ranked #1, got %s", result.Candidates[0].Ticker)
	}
	if result.Candidates[0].Rank != 1 {
		t.Errorf("expected rank 1, got %d", result.Candidates[0].Rank)
	}
	if result.Candidates[1].Rank != 2 {
		t.Errorf("expected rank 2, got %d", result.Candidates[1].Rank)
	}

	if result.Candidates[0].OwnerYield <= result.Candidates[1].OwnerYield {
		t.Errorf("expected YIELD_A owner yield (%f) > YIELD_B owner yield (%f)",
			result.Candidates[0].OwnerYield, result.Candidates[1].OwnerYield)
	}

	if !result.Candidates[0].HasMarketCap || result.Candidates[0].MarketCap != 10_000_000 {
		t.Errorf("unexpected market cap for YIELD_A: %f", result.Candidates[0].MarketCap)
	}

	// Test hurdle filter
	hurdleResult, err := ScreenOwnerYield(ctx, s, []string{"YIELD_A", "YIELD_B"}, marketCaps, prices, OwnerYieldFilter{
		Years:             3,
		MinRevenue:        1000,
		RequireHurdleBeat: true,
	})
	if err != nil {
		t.Fatalf("ScreenOwnerYield with hurdle error: %v", err)
	}
	for _, cand := range hurdleResult.Candidates {
		if !cand.BeatsHurdle {
			t.Errorf("expected candidate %s to beat hurdle", cand.Ticker)
		}
	}

	var out bytes.Buffer
	if err := WriteOwnerYieldTable(&out, result); err != nil {
		t.Fatalf("WriteOwnerYieldTable failed: %v", err)
	}
	if !strings.Contains(out.String(), "YIELD_A") || !strings.Contains(out.String(), "OWNER YLD") {
		t.Fatalf("unexpected owner yield table:\n%s", out.String())
	}
}
