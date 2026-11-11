package valuation

import (
	"math"
	"testing"

	"sec/internal/fundamentals"
)

func TestCalculateDCFFadesGrowthCapExWedge(t *testing.T) {
	summary := testFinancialSummary(map[string][]fundamentals.SummaryValue{
		"Revenue":                     testValues("USD", 200, 180, 160, 140),
		"Operating Cash Flow":         testValues("USD", 100, 90, 80, 70),
		"Capital Expenditure":         testValues("USD", 30, 25, 22, 20),
		"Depreciation & Amortization": testValues("USD", 20, 18, 16, 14),
		"Free Cash Flow":              testValues("USD", 70, 65, 58, 50),
		"Cash & Equivalents":          testValues("USD", 40, 35, 30, 25),
		"Debt / Borrowings":           testValues("USD", 20, 25, 30, 35),
		"Shares Outstanding":          testValues("shares", 10, 10, 10, 10),
	})
	result, err := CalculateDCF(summary, DCFConfig{
		Mode: DCFModeFCF, GrowthRate: 0.10, DiscountRate: 0.10,
		TerminalRate: 0.02, Years: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !closeEnough(result.BaseFCF, 70) {
		t.Fatalf("base FCF = %v, want 70", result.BaseFCF)
	}
	if got := result.Projections[0].FreeCashFlow; !closeEnough(got, 82.5) {
		t.Fatalf("year 1 FCF = %v, want 82.5 after half the growth CapEx wedge fades", got)
	}
	if got := result.Projections[1].FreeCashFlow; !closeEnough(got, 96.8) {
		t.Fatalf("year 2 FCF = %v, want 96.8 after the growth CapEx wedge reaches zero", got)
	}
	if !closeEnough(result.Projections[0].CapExWedge, 5.5) || !closeEnough(result.Projections[1].CapExWedge, 0) {
		t.Fatalf("CapEx wedge did not fade linearly: %#v", result.Projections)
	}
	if !closeEnough(result.EnterpriseValue, result.PVExplicitCashFlows+result.PVTerminalValue) {
		t.Fatalf("enterprise value %v does not equal explicit plus terminal present value", result.EnterpriseValue)
	}
}

func TestCalculateDCFRevenueBuildsFCFF(t *testing.T) {
	summary := testFinancialSummary(map[string][]fundamentals.SummaryValue{
		"Revenue":                     testValues("USD", 120, 110, 100, 90),
		"Operating Income":            testValues("USD", 30, 27.5, 25, 22.5),
		"Operating Cash Flow":         testValues("USD", 25, 23, 21, 19),
		"Capital Expenditure":         testValues("USD", 24, 22, 20, 18),
		"Depreciation & Amortization": testValues("USD", 12, 11, 10, 9),
		"Free Cash Flow":              testValues("USD", 1, 1, 1, 1),
		"Current Assets":              testValues("USD", 30, 27.5, 25, 22.5),
		"Current Liabilities":         testValues("USD", 18, 16.5, 15, 13.5),
		"Cash & Equivalents":          testValues("USD", 40, 35, 30, 25),
		"Debt / Borrowings":           testValues("USD", 20, 25, 30, 35),
		"Shares Outstanding":          testValues("shares", 10, 10, 10, 10),
	})
	result, err := CalculateDCF(summary, DCFConfig{
		Mode: DCFModeRevenue, GrowthRate: 0.10, DiscountRate: 0.10,
		TerminalRate: 0.02, TaxRate: 0.25, Years: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := result.Projections[0]
	if !closeEnough(first.Revenue, 132) || !closeEnough(first.EBIT, 33) || !closeEnough(first.NOPAT, 24.75) {
		t.Fatalf("unexpected year 1 revenue/earnings build: %#v", first)
	}
	if !closeEnough(first.DepreciationAmortization, 13.2) || !closeEnough(first.CapitalExpenditure, 19.8) || !closeEnough(first.ChangeInNWC, 1.2) {
		t.Fatalf("unexpected year 1 reinvestment build: %#v", first)
	}
	if !closeEnough(first.FreeCashFlow, 16.95) || !closeEnough(first.PresentValue, 16.95/1.1) {
		t.Fatalf("year 1 FCFF/PV = %v/%v, want 16.95/%v", first.FreeCashFlow, first.PresentValue, 16.95/1.1)
	}
}

func testFinancialSummary(metrics map[string][]fundamentals.SummaryValue) fundamentals.FinancialSummary {
	periods := []fundamentals.SummaryPeriod{
		{Year: "2025", End: "2025-12-31"},
		{Year: "2024", End: "2024-12-31"},
		{Year: "2023", End: "2023-12-31"},
		{Year: "2022", End: "2022-12-31"},
	}
	summary := fundamentals.FinancialSummary{Company: "Example Corp", Ticker: "EX", Periods: periods}
	for name, values := range metrics {
		summary.Metrics = append(summary.Metrics, fundamentals.SummaryMetric{Name: name, Values: values})
	}
	return summary
}

func testValues(unit string, values ...float64) []fundamentals.SummaryValue {
	result := make([]fundamentals.SummaryValue, len(values))
	for i, value := range values {
		result[i] = fundamentals.SummaryValue{Value: value, Unit: unit, Present: true}
	}
	return result
}

func closeEnough(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
