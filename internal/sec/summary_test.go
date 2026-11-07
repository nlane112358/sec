package sec

import "testing"

func TestDeriveOperatingIncomeFromReportedComponents(t *testing.T) {
	summary := FinancialSummary{Metrics: []SummaryMetric{
		{Name: "Revenue", Values: testValues("USD", 100)},
		{Name: "Operating Income", Values: []SummaryValue{{Unit: "USD"}}},
		{Name: "Cost of Revenue", Values: testValues("USD", 40)},
		{Name: "Selling, General & Administrative", Values: testValues("USD", 20)},
		{Name: "Research & Development", Values: testValues("USD", 10)},
		{Name: "Goodwill & Intangible Impairment", Values: testValues("USD", 2)},
	}}
	deriveOperatingIncome(&summary)
	got := namedMetric(summary, "Operating Income")[0]
	if !got.Present || got.Value != 28 {
		t.Fatalf("derived operating income = %+v, want USD 28", got)
	}
}

func TestDeriveOperatingIncomeLeavesIncompleteFactsMissing(t *testing.T) {
	summary := FinancialSummary{Metrics: []SummaryMetric{
		{Name: "Revenue", Values: testValues("USD", 100)},
		{Name: "Operating Income", Values: []SummaryValue{{Unit: "USD"}}},
		{Name: "Cost of Revenue", Values: testValues("USD", 40)},
		{Name: "Selling, General & Administrative", Values: testValues("USD", 20)},
	}}
	deriveOperatingIncome(&summary)
	got := namedMetric(summary, "Operating Income")[0]
	if got.Present {
		t.Fatalf("incomplete operating expense components should stay missing, got %+v", got)
	}
}
