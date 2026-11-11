package valuation

import (
	"testing"

	"sec/internal/fundamentals"
)

func TestCalculateWACCWeightsAfterCash(t *testing.T) {
	summary := testFinancialSummary(map[string][]fundamentals.SummaryValue{
		"Debt / Borrowings":    testValues("USD", 100, 100, 100, 100),
		"Cash & Equivalents":   testValues("USD", 20, 20, 20, 20),
		"Stockholders' Equity": testValues("USD", 300, 300, 300, 300),
	})
	result, err := CalculateWACC(summary, WACCConfig{
		RiskFreeRate: 0.04, EquityRiskPremium: 0.05, Beta: 1.2,
		CostOfDebt: 0.06, TaxRate: 0.25, MarketCap: 400,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !closeEnough(result.CostOfEquity, 0.10) {
		t.Fatalf("cost of equity = %v, want 0.10", result.CostOfEquity)
	}
	if !closeEnough(result.Debt, 80) || !closeEnough(result.EquityWeight, 400.0/480) || !closeEnough(result.DebtWeight, 80.0/480) {
		t.Fatalf("unexpected capital weights: %#v", result)
	}
	want := (400.0/480)*0.10 + (80.0/480)*0.06*(1-0.25)
	if !closeEnough(result.Rate, want) {
		t.Fatalf("WACC = %.12f, want %.12f", result.Rate, want)
	}
}
