package edgar

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"sec/internal/store"
)

type FactEntry struct {
	Accession string  `json:"accn"`
	End       string  `json:"end"`
	Start     string  `json:"start,omitempty"`
	Form      string  `json:"form"`
	Filed     string  `json:"filed"`
	FY        *int    `json:"fy,omitempty"`
	FP        string  `json:"fp,omitempty"`
	Frame     string  `json:"frame,omitempty"`
	Value     float64 `json:"val"`
}

type CompanyFacts struct {
	CIK    int    `json:"cik"`
	Entity string `json:"entityName"`
	Facts  map[string]map[string]struct {
		Label       string                 `json:"label"`
		Description string                 `json:"description"`
		Units       map[string][]FactEntry `json:"units"`
	} `json:"facts"`
}

func (facts *CompanyFacts) UnmarshalJSON(data []byte) error {
	type companyFactsAlias CompanyFacts
	var decoded struct {
		CIK json.RawMessage `json:"cik"`
		*companyFactsAlias
	}
	decoded.companyFactsAlias = (*companyFactsAlias)(facts)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	cikText := strings.TrimSpace(string(decoded.CIK))
	if cikText == "" || cikText == "null" {
		facts.CIK = 0
		return nil
	}
	if strings.HasPrefix(cikText, `"`) {
		if err := json.Unmarshal(decoded.CIK, &cikText); err != nil {
			return fmt.Errorf("decode company facts CIK: %w", err)
		}
	}
	value, err := strconv.ParseInt(cikText, 10, strconv.IntSize)
	if err != nil {
		return fmt.Errorf("decode company facts CIK %q: %w", cikText, err)
	}
	facts.CIK = int(value)
	return nil
}

type SICCode string

func (code *SICCode) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || len(data) == 0 {
		*code = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*code = SICCode(strings.TrimSpace(text))
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("decode SIC code: %w", err)
	}
	parsed, err := strconv.ParseInt(number.String(), 10, 32)
	if err != nil {
		return fmt.Errorf("decode SIC code %q: %w", number, err)
	}
	*code = SICCode(strconv.FormatInt(parsed, 10))
	return nil
}

type CompanySubmissions struct {
	CIK            string            `json:"cik"`
	Name           string            `json:"name"`
	SIC            SICCode           `json:"sic"`
	SICDescription string            `json:"sicDescription"`
	Filings        SubmissionFilings `json:"filings"`
}

type SubmissionFilings struct {
	Recent       FilingHistory            `json:"recent"`
	Files        []SupplementalFilingFile `json:"files"`
	Supplemental []FilingHistory          `json:"-"`
}

type SupplementalFilingFile struct {
	Name        string `json:"name"`
	FilingFrom  string `json:"filingFrom"`
	FilingTo    string `json:"filingTo"`
	FilingCount int    `json:"filingCount"`
}

type FilingHistory struct {
	AccessionNumber       []string `json:"accessionNumber"`
	FilingDate            []string `json:"filingDate"`
	ReportDate            []string `json:"reportDate"`
	Form                  []string `json:"form"`
	PrimaryDocument       []string `json:"primaryDocument"`
	PrimaryDocDescription []string `json:"primaryDocDescription"`
}

func (s *CompanySubmissions) RecentFilings(cik string) []store.Filing {
	if s == nil {
		return nil
	}
	return filingsFromHistory(cik, s.Filings.Recent)
}

func (s *CompanySubmissions) AllFilings(cik string) []store.Filing {
	if s == nil {
		return nil
	}
	histories := make([]FilingHistory, 0, 1+len(s.Filings.Supplemental))
	histories = append(histories, s.Filings.Recent)
	histories = append(histories, s.Filings.Supplemental...)
	seen := make(map[string]struct{})
	filings := make([]store.Filing, 0)
	for _, history := range histories {
		for _, filing := range filingsFromHistory(cik, history) {
			if _, ok := seen[filing.Accession]; ok {
				continue
			}
			seen[filing.Accession] = struct{}{}
			filings = append(filings, filing)
		}
	}
	sort.Slice(filings, func(i, j int) bool {
		if filings[i].FilingDate != filings[j].FilingDate {
			return filings[i].FilingDate > filings[j].FilingDate
		}
		return filings[i].Accession > filings[j].Accession
	})
	return filings
}

func filingsFromHistory(cik string, h FilingHistory) []store.Filing {
	count := len(h.AccessionNumber)
	for _, n := range []int{len(h.FilingDate), len(h.Form), len(h.PrimaryDocument)} {
		if n < count {
			count = n
		}
	}
	filings := make([]store.Filing, 0, count)
	for i := 0; i < count; i++ {
		accession := strings.TrimSpace(h.AccessionNumber[i])
		primaryDocument := strings.TrimSpace(h.PrimaryDocument[i])
		if accession == "" || primaryDocument == "" || strings.Contains(primaryDocument, "..") || strings.ContainsAny(primaryDocument, "/\\") {
			continue
		}
		filing := store.Filing{
			CIK:             cik,
			Accession:       accession,
			Form:            h.Form[i],
			FilingDate:      h.FilingDate[i],
			PrimaryDocument: primaryDocument,
		}
		if i < len(h.ReportDate) {
			filing.ReportDate = h.ReportDate[i]
		}
		if i < len(h.PrimaryDocDescription) {
			filing.DocumentTitle = h.PrimaryDocDescription[i]
		}
		filings = append(filings, filing)
	}
	return filings
}

var validCIK = regexp.MustCompile(`^[0-9]{1,10}$`)

func NormaliseCIK(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !validCIK.MatchString(raw) {
		return "", fmt.Errorf("invalid CIK %q", raw)
	}
	return strings.Repeat("0", 10-len(raw)) + raw, nil
}

func ExtractFacts(data *CompanyFacts, cik string) []store.FinancialFact {
	var out []store.FinancialFact
	if data == nil {
		return out
	}

	for _, taxonomy := range []string{"us-gaap", "ifrs-full", "dei"} {
		definitions, ok := data.Facts[taxonomy]
		if !ok {
			continue
		}
		for concept, def := range definitions {
			for unit, entries := range def.Units {
				if !isAllowedFactUnit(unit) {
					continue
				}
				for _, e := range entries {
					if !secFilingForm(e.Form) {
						continue
					}
					out = append(out, store.FinancialFact{
						Company:    data.Entity,
						CIK:        cik,
						Taxonomy:   taxonomy,
						Concept:    concept,
						Label:      def.Label,
						Unit:       unit,
						Value:      e.Value,
						Start:      e.Start,
						End:        e.End,
						FilingDate: e.Filed,
						Form:       e.Form,
						Accession:  e.Accession,
						Frame:      e.Frame,
					})
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].End != out[j].End {
			return out[i].End > out[j].End
		}
		if out[i].Concept != out[j].Concept {
			return out[i].Concept < out[j].Concept
		}
		if out[i].Taxonomy != out[j].Taxonomy {
			return out[i].Taxonomy < out[j].Taxonomy
		}
		return out[i].FilingDate > out[j].FilingDate
	})

	return out
}

func isAllowedFactUnit(unit string) bool {
	u := strings.ToLower(strings.TrimSpace(unit))
	if u == "usd" || u == "shares" || u == "pure" || u == "usd/shares" {
		return true
	}
	if len(u) == 3 {
		allAlpha := true
		for _, r := range u {
			if r < 'a' || r > 'z' {
				allAlpha = false
				break
			}
		}
		return allAlpha
	}
	return false
}

func secFilingForm(form string) bool {
	form = strings.TrimSuffix(form, "/A")
	switch form {
	case "10-K", "10-Q", "20-F", "40-F", "6-K":
		return true
	default:
		return false
	}
}
