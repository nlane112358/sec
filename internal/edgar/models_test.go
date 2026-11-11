package edgar

import (
	"encoding/json"
	"testing"

	"sec/internal/store"
)

func TestExtractFactsParsesTaxonomiesAndAmendments(t *testing.T) {
	const payload = `{
  "cik": 1234,
  "entityName": "Example Corp",
  "facts": {
    "us-gaap": {
      "Revenues": {"label":"Revenue","units":{"USD":[
        {"accn":"0001-25-000001","start":"2025-01-01","end":"2025-12-31","form":"10-K/A","filed":"2026-02-01","val":125000000},
        {"accn":"0001-25-000002","start":"2025-01-01","end":"2025-12-31","form":"8-K","filed":"2026-02-02","val":125000000}
      ]}}
    },
    "ifrs-full": {
      "Revenue": {"label":"Revenue","units":{"USD":[
        {"accn":"0002-25-000001","start":"2025-01-01","end":"2025-12-31","form":"20-F","filed":"2026-03-01","val":90000000}
      ]}}
    },
    "dei": {
      "EntityCommonStockSharesOutstanding": {"label":"Entity Common Stock, Shares Outstanding","units":{"shares":[
        {"accn":"0001-25-000001","end":"2025-12-31","form":"10-K","filed":"2026-02-01","val":50000000}
      ]}}
    }
  }
}`
	var data CompanyFacts
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	facts := ExtractFacts(&data, "0000001234")
	if len(facts) != 3 {
		t.Fatalf("got %d facts, want 3 accepted facts (including amendment, excluding 8-K)", len(facts))
	}
	seen := map[string]store.FinancialFact{}
	for _, fact := range facts {
		seen[fact.Taxonomy+":"+fact.Concept] = fact
		if fact.CIK != "0000001234" {
			t.Errorf("fact CIK = %q, want zero-padded CIK", fact.CIK)
		}
	}
	if fact, ok := seen["us-gaap:Revenues"]; !ok || fact.Form != "10-K/A" || fact.Value != 125000000 {
		t.Errorf("amended us-gaap revenue was not parsed: %#v", fact)
	}
	if fact, ok := seen["ifrs-full:Revenue"]; !ok || fact.Form != "20-F" || fact.Value != 90000000 {
		t.Errorf("IFRS revenue was not parsed: %#v", fact)
	}
	if fact, ok := seen["dei:EntityCommonStockSharesOutstanding"]; !ok || fact.Unit != "shares" || fact.Value != 50000000 {
		t.Errorf("DEI shares outstanding were not parsed: %#v", fact)
	}
}

func TestCompanySubmissionsAcceptsNumericSIC(t *testing.T) {
	var submissions CompanySubmissions
	if err := json.Unmarshal([]byte(`{"sic": 6021, "sicDescription":"National Commercial Banks"}`), &submissions); err != nil {
		t.Fatal(err)
	}
	if string(submissions.SIC) != "6021" || submissions.SICDescription != "National Commercial Banks" {
		t.Fatalf("parsed SIC metadata = %q %q", submissions.SIC, submissions.SICDescription)
	}
}

func TestCompanyFactsAcceptsStringCIK(t *testing.T) {
	var facts CompanyFacts
	if err := json.Unmarshal([]byte(`{"cik":"1959379","entityName":"Example","facts":{}}`), &facts); err != nil {
		t.Fatal(err)
	}
	if facts.CIK != 1959379 {
		t.Fatalf("parsed CIK = %d, want 1959379", facts.CIK)
	}
}
