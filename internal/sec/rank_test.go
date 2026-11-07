package sec

import (
	"strconv"
	"testing"
)

func TestRankCandidateUsesSplitAdjustedDilutedSharesAndProposedFactors(t *testing.T) {
	summary := rankTestSummary(map[string][]SummaryValue{
		"Revenue":                         testValues("USD", 200e6, 180e6, 160e6, 140e6, 120e6, 100e6),
		"Operating Income":                testValues("USD", 40e6, 36e6, 32e6, 28e6, 24e6, 20e6),
		"Net Income":                      testValues("USD", 30e6, 27e6, 24e6, 21e6, 18e6, 15e6),
		"Operating Cash Flow":             testValues("USD", 45e6, 40e6, 35e6, 30e6, 25e6, 20e6),
		"Free Cash Flow":                  testValues("USD", 40e6, 36e6, 32e6, 28e6, 24e6, 20e6),
		"SBC-Adjusted FCF Proxy":          testValues("USD", 30e6, 27e6, 24e6, 21e6, 18e6, 15e6),
		"Stock-Based Compensation":        testValues("USD", 10e6, 9e6, 8e6, 7e6, 6e6, 5e6),
		"Stockholders' Equity":            testValues("USD", 120e6, 110e6, 100e6, 90e6, 80e6, 70e6),
		"Cash & Equivalents":              testValues("USD", 10e6, 10e6, 10e6, 10e6, 10e6, 10e6),
		"Debt / Borrowings":               testValues("USD", 20e6, 20e6, 20e6, 20e6, 20e6, 20e6),
		"Total Assets":                    testValues("USD", 200e6, 190e6, 180e6, 170e6, 160e6, 150e6),
		"Income Tax Expense":              testValues("USD", 5.25e6, 4.725e6, 4.2e6, 3.675e6, 3.15e6, 2.625e6),
		"Income Before Tax":               testValues("USD", 25e6, 22.5e6, 20e6, 17.5e6, 15e6, 12.5e6),
		"Diluted Weighted Average Shares": testValues("shares", 100e6, 100e6, 100e6, 100e6, 100e6, 100e6),
		// Point-in-time DEI shares can cross a split and must not drive rank trends.
		"Shares Outstanding": testValues("shares", 5e9, 5e9, 5e9, 5e9, 5e9, 5e9),
	})
	candidate, ok := makeRankCandidate(summary, 100e6)
	if !ok {
		t.Fatal("expected candidate to pass the fundamentals screen")
	}
	if !candidate.company.HasSBCAdjustment {
		t.Fatal("expected matching SBC facts to be used")
	}
	if !closeEnough(candidate.company.FCFMargin, 0.2) {
		t.Fatalf("reported FCF margin = %v, want 0.2", candidate.company.FCFMargin)
	}
	if !closeEnough(candidate.company.OwnerEarningsMargin, 0.15) {
		t.Fatalf("SBC-adjusted margin = %v, want 0.15", candidate.company.OwnerEarningsMargin)
	}
	if !candidate.company.HasROIC || candidate.company.ROIC <= 0 {
		t.Fatalf("expected positive ROIC, got available=%v value=%v", candidate.company.HasROIC, candidate.company.ROIC)
	}
	if !candidate.company.HasEPSGrowth || !candidate.company.HasShareTrend {
		t.Fatal("expected per-share growth and diluted share trend factors")
	}
	if !closeEnough(candidate.company.ShareReduction, 0) {
		t.Fatalf("diluted share reduction = %v, want 0", candidate.company.ShareReduction)
	}
	if _, ok := candidate.factors["roic"]; !ok {
		t.Fatal("nonfinancial ranking should include ROIC")
	}
}

func TestAdjustSplitShareSeriesRestatesHistoricalCounts(t *testing.T) {
	values := testValues("shares", 1.35e9, 1.38e9, 1.40e9, 28.5e6, 28.4e6)
	got, adjusted := adjustSplitShareSeries(values)
	if !adjusted {
		t.Fatal("expected the 50-for-1 discontinuity to be detected")
	}
	if !closeEnough(got[3].Value, 1.425e9) || !closeEnough(got[4].Value, 1.42e9) {
		t.Fatalf("historical shares were not restated: %v", got)
	}
	if _, adjusted := adjustSplitShareSeries(testValues("shares", 105, 100, 95)); adjusted {
		t.Fatal("ordinary 5% share movement should not be treated as a split")
	}
}

func TestRankCandidateKeepsMissingOperatingMarginMissing(t *testing.T) {
	summary := rankTestSummary(map[string][]SummaryValue{
		"Revenue":                         testValues("USD", 200e6, 180e6, 160e6, 140e6, 120e6, 100e6),
		"Net Income":                      testValues("USD", 30e6, 27e6, 24e6, 21e6, 18e6, 15e6),
		"Operating Cash Flow":             testValues("USD", 45e6, 40e6, 35e6, 30e6, 25e6, 20e6),
		"Diluted Weighted Average Shares": testValues("shares", 100e6, 100e6, 100e6, 100e6, 100e6, 100e6),
	})
	candidate, ok := makeRankCandidate(summary, 100e6)
	if !ok {
		t.Fatal("expected candidate to remain rankable with other available factors")
	}
	if candidate.company.HasOperatingMargin {
		t.Fatal("missing operating-income facts must not become a zero margin")
	}
	if _, exists := candidate.factors["op_margin"]; exists {
		t.Fatal("missing operating margin should not enter the score as zero")
	}
}

func TestPeerGroupsUseSeparateFinancialAndIndustryPercentiles(t *testing.T) {
	candidates := []rankCandidate{
		{company: RankedCompany{Sector: "Financials", PeerGroup: "Financial SIC 63"}, factors: map[string]float64{"roe": 0.10, "growth": 0.10}, percentile: map[string]float64{}},
		{company: RankedCompany{Sector: "Financials", PeerGroup: "Financial SIC 63"}, factors: map[string]float64{"roe": 0.20, "growth": 0.20}, percentile: map[string]float64{}},
		{company: RankedCompany{Sector: "Nonfinancial", PeerGroup: "SIC 35"}, factors: map[string]float64{"roic": 0.80, "growth": 0.80}, percentile: map[string]float64{}},
		{company: RankedCompany{Sector: "Nonfinancial", PeerGroup: "SIC 35"}, factors: map[string]float64{"roic": 0.90, "growth": 0.90}, percentile: map[string]float64{}},
	}
	assignRankPercentiles(candidates)
	if !closeEnough(candidates[0].company.Score, 0) || !closeEnough(candidates[1].company.Score, 100) {
		t.Fatalf("financial cohort should rank independently: %.1f, %.1f", candidates[0].company.Score, candidates[1].company.Score)
	}
	if !closeEnough(candidates[2].company.Score, 0) || !closeEnough(candidates[3].company.Score, 100) {
		t.Fatalf("SIC industry cohort should rank independently: %.1f, %.1f", candidates[2].company.Score, candidates[3].company.Score)
	}
}

func TestSmallPeerGroupsBroadenToSICDivision(t *testing.T) {
	candidates := []rankCandidate{
		{company: RankedCompany{Sector: "Nonfinancial", PeerGroup: "SIC 35"}},
		{company: RankedCompany{Sector: "Nonfinancial", PeerGroup: "SIC 36"}},
		{company: RankedCompany{Sector: "Financials", PeerGroup: "Financial SIC 63"}},
	}
	broadenSmallPeerGroups(candidates, 5)
	if candidates[0].company.PeerGroup != "SIC division 3" || candidates[1].company.PeerGroup != "SIC division 3" {
		t.Fatalf("small nonfinancial SIC groups were not broadened: %q, %q", candidates[0].company.PeerGroup, candidates[1].company.PeerGroup)
	}
	if candidates[2].company.PeerGroup != "Financials" {
		t.Fatalf("small financial SIC group should remain in the financial cohort, got %q", candidates[2].company.PeerGroup)
	}
}

func TestSectorAndPeerGroupFromSIC(t *testing.T) {
	for _, test := range []struct {
		sic       string
		sector    string
		peerGroup string
	}{
		{sic: "6021", sector: "Financials", peerGroup: "Financial SIC 60"},
		{sic: "6311", sector: "Financials", peerGroup: "Financial SIC 63"},
		{sic: "7372", sector: "Nonfinancial", peerGroup: "SIC 73"},
		{sic: "", sector: "Unclassified", peerGroup: "Unclassified"},
	} {
		if got := sectorFromSIC(test.sic); got != test.sector {
			t.Errorf("sectorFromSIC(%q) = %q, want %q", test.sic, got, test.sector)
		}
		if got := peerGroupFromSIC(test.sic); got != test.peerGroup {
			t.Errorf("peerGroupFromSIC(%q) = %q, want %q", test.sic, got, test.peerGroup)
		}
	}
}

func rankTestSummary(metrics map[string][]SummaryValue) FinancialSummary {
	periods := make([]SummaryPeriod, 6)
	for i := range periods {
		year := strconv.Itoa(2025 - i)
		periods[i] = SummaryPeriod{Year: year, End: year + "-12-31"}
	}
	summary := FinancialSummary{Ticker: "EX", Company: "Example Corp", Periods: periods}
	for name, values := range metrics {
		summary.Metrics = append(summary.Metrics, SummaryMetric{Name: name, Values: values})
	}
	return summary
}
