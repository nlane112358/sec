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

func TestIntangibleCapitalizationModel(t *testing.T) {
	// 5 years: t=0 (latest), t=1, t=2, t=3, t=4
	// RD = 50, 40, 30, 20, 10
	// SGA = 30, 30, 30, 30, 30
	// Operating Income = 100, 90, 80, 70, 60
	// Equity = 200, 180, 160, 140, 120
	// Debt = 50, Cash = 20
	periods := []SummaryPeriod{
		{Year: "2025", End: "2025-12-31"},
		{Year: "2024", End: "2024-12-31"},
		{Year: "2023", End: "2023-12-31"},
		{Year: "2022", End: "2022-12-31"},
		{Year: "2021", End: "2021-12-31"},
	}
	summary := FinancialSummary{
		Periods: periods,
		Metrics: []SummaryMetric{
			{Name: "Operating Income", Values: testValues("USD", 100, 90, 80, 70, 60)},
			{Name: "Research & Development", Values: testValues("USD", 50, 40, 30, 20, 10)},
			{Name: "Selling, General & Administrative", Values: testValues("USD", 30, 30, 30, 30, 30)},
			{Name: "Stockholders' Equity", Values: testValues("USD", 200, 180, 160, 140, 120)},
			{Name: "Debt / Borrowings", Values: testValues("USD", 50, 50, 50, 50, 50)},
			{Name: "Cash & Equivalents", Values: testValues("USD", 20, 20, 20, 20, 20)},
		},
	}
	deriveIntangibleMetrics(&summary, nil, nil)

	// Check Intangible Capital at t=0:
	// RD Capital = 1.0*50 + 0.8*40 + 0.6*30 + 0.4*20 + 0.2*10
	//            = 50 + 32 + 18 + 8 + 2 = 110
	// SGA Capital (30% of SGA):
	// SGA_0 = 30*0.30 = 9; SGA_1 = 9; SGA_2 = 9
	// SGA Capital = 1.0*9 + (2/3)*9 + (1/3)*9 = 9 + 6 + 3 = 18
	// Total Intangible Capital = 110 + 18 = 128
	intCap := namedMetric(summary, "Intangible Capital")[0]
	if !intCap.Present || !closeEnough(intCap.Value, 128) {
		t.Fatalf("Intangible Capital[0] = %v, want 128", intCap.Value)
	}

	// Check Amortization at t=0:
	// RD Amort = 0.2*(40 + 30 + 20 + 10) = 0.2*100 = 20
	// SGA Amort = (1/3)*(0.30*30 + 0.30*30 + 0.30*30) = 0.10*(30+30+30) = 9
	// Total Amortization = 20 + 9 = 29
	amort := namedMetric(summary, "Amortization of Intangibles")[0]
	if !amort.Present || !closeEnough(amort.Value, 29) {
		t.Fatalf("Amortization[0] = %v, want 29", amort.Value)
	}

	// Check Adjusted EBIT at t=0:
	// Adjusted EBIT = Operating Income + RD_0 + 0.30*SGA_0 - Amortization
	//               = 100 + 50 + 9 - 29 = 130
	adjEBIT := namedMetric(summary, "Adjusted Operating Income")[0]
	if !adjEBIT.Present || !closeEnough(adjEBIT.Value, 130) {
		t.Fatalf("Adjusted EBIT[0] = %v, want 130", adjEBIT.Value)
	}

	// Check Adjusted Invested Capital at t=0:
	// Invested Capital = Equity (200) + Debt (50) - Cash (20) + Intangible Capital (128) = 358
	adjIC := namedMetric(summary, "Adjusted Invested Capital")[0]
	if !adjIC.Present || !closeEnough(adjIC.Value, 358) {
		t.Fatalf("Adjusted Invested Capital[0] = %v, want 358", adjIC.Value)
	}

	// Check Intangible Capital at t=1:
	// RD Capital = 1.0*40 + 0.8*30 + 0.6*20 + 0.4*10 = 40 + 24 + 12 + 4 = 80
	// SGA Capital = 1.0*9 + (2/3)*9 + (1/3)*9 = 18
	// Total = 80 + 18 = 98
	intCap1 := namedMetric(summary, "Intangible Capital")[1]
	if !intCap1.Present || !closeEnough(intCap1.Value, 98) {
		t.Fatalf("Intangible Capital[1] = %v, want 98", intCap1.Value)
	}

	// Check Capital Identity: Capital_0 = Capital_1 + Investment_0 - Amortization_0
	// 98 + (50 + 9) - 29 = 98 + 59 - 29 = 128!
	if !closeEnough(intCap1.Value+(50+9)-amort.Value, intCap.Value) {
		t.Fatalf("capital identity violated: %v + 59 - %v != %v", intCap1.Value, amort.Value, intCap.Value)
	}

	calc := CalculateIntangibles(summary)
	if len(calc.IntangibleCapital) != len(periods) || !closeEnough(calc.IntangibleCapital[0].Value, 128) {
		t.Fatalf("CalculateIntangibles capital mismatch: %+v", calc)
	}
	if len(calc.AdjustedEBIT) != len(periods) || !closeEnough(calc.AdjustedEBIT[0].Value, 130) {
		t.Fatalf("CalculateIntangibles EBIT mismatch: %+v", calc)
	}
}

func TestBetterSummaryFactDualClassShareSelection(t *testing.T) {
	// Dual-class share scenario (e.g. ERIE: 46.2M Class A vs 2,542 Class B on same filing)
	current := summaryFact{
		form:      "10-K",
		end:       "2025-12-31",
		filed:     "2026-02-20",
		accession: "0000922621-26-000010",
		unit:      "shares",
		value:     2542,
	}
	candidate := summaryFact{
		form:      "10-K",
		end:       "2025-12-31",
		filed:     "2026-02-20",
		accession: "0000922621-26-000010",
		unit:      "shares",
		value:     46200000,
	}
	if !betterSummaryFact(candidate, current, "2025-12-31") {
		t.Errorf("betterSummaryFact should prefer candidate with larger share count (46.2M > 2542)")
	}
	if betterSummaryFact(current, candidate, "2025-12-31") {
		t.Errorf("betterSummaryFact should not prefer smaller share count candidate (2542 < 46.2M)")
	}
}

func TestSanitizeSharesOutstandingDualClass(t *testing.T) {
	summary := FinancialSummary{
		Periods: []SummaryPeriod{{Year: "2025", End: "2025-12-31"}},
		Metrics: []SummaryMetric{
			{Name: "Shares Outstanding", Values: []SummaryValue{{Value: 2542, Unit: "shares", Present: true}}},
			{Name: "Diluted Weighted Average Shares", Values: []SummaryValue{{Value: 46200000, Unit: "shares", Present: true}}},
		},
	}
	sanitizeSharesOutstanding(&summary)
	got := namedMetric(summary, "Shares Outstanding")[0]
	if !got.Present || got.Value != 46200000 {
		t.Fatalf("sanitizeSharesOutstanding should replace 2542 with 46.2M diluted shares, got %v", got.Value)
	}
}

func TestSelectSGASplitTags(t *testing.T) {
	facts := []summaryFact{
		{
			taxonomy:  "us-gaap",
			concept:   "SellingAndMarketingExpense",
			start:     "2025-01-01",
			end:       "2025-12-31",
			filed:     "2026-02-15",
			form:      "10-K",
			unit:      "USD",
			value:     1200000,
			accession: "0001-26-0001",
		},
		{
			taxonomy:  "us-gaap",
			concept:   "GeneralAndAdministrativeExpense",
			start:     "2025-01-01",
			end:       "2025-12-31",
			filed:     "2026-02-15",
			form:      "10-K",
			unit:      "USD",
			value:     800000,
			accession: "0001-26-0001",
		},
	}
	sga := selectSGA(facts, "2025-12-31", sgaConcepts)
	if !sga.Present || sga.Value != 2000000 {
		t.Fatalf("selectSGA should sum split S&M (1.2M) and G&A (0.8M) to 2.0M, got %+v", sga)
	}
}

func TestDeriveIntangibleMetricsWithHistoricalGaps(t *testing.T) {
	// 5 periods where year 1 (t=1) R&D is missing, but year 0 (50) and year 2 (30) exist
	// Interpolation should bridge year 1 to (50 + 30)/2 = 40, avoiding a zeroed out Intangible Capital
	periods := []SummaryPeriod{
		{Year: "2025", End: "2025-12-31"},
		{Year: "2024", End: "2024-12-31"},
		{Year: "2023", End: "2023-12-31"},
		{Year: "2022", End: "2022-12-31"},
		{Year: "2021", End: "2021-12-31"},
	}
	summary := FinancialSummary{
		Periods: periods,
		Metrics: []SummaryMetric{
			{Name: "Operating Income", Values: testValues("USD", 100, 90, 80, 70, 60)},
			{Name: "Research & Development", Values: []SummaryValue{
				{Value: 50, Unit: "USD", Present: true},
				{Value: 0, Unit: "USD", Present: false}, // missing year
				{Value: 30, Unit: "USD", Present: true},
				{Value: 20, Unit: "USD", Present: true},
				{Value: 10, Unit: "USD", Present: true},
			}},
			{Name: "Selling, General & Administrative", Values: testValues("USD", 30, 30, 30, 30, 30)},
			{Name: "Stockholders' Equity", Values: testValues("USD", 200, 180, 160, 140, 120)},
			{Name: "Debt / Borrowings", Values: testValues("USD", 50, 50, 50, 50, 50)},
			{Name: "Cash & Equivalents", Values: testValues("USD", 20, 20, 20, 20, 20)},
		},
	}
	deriveIntangibleMetrics(&summary, nil, nil)
	intCap := namedMetric(summary, "Intangible Capital")[0]
	if !intCap.Present || intCap.Value <= 0 {
		t.Fatalf("Intangible Capital should not be zeroed out when historical R&D has gaps, got %+v", intCap)
	}
	if !closeEnough(intCap.Value, 128) {
		t.Fatalf("Intangible Capital[0] with interpolated year = %v, want 128", intCap.Value)
	}
}
