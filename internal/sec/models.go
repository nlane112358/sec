package sec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
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

type CompanySubmissions struct {
	CIK     string            `json:"cik"`
	Name    string            `json:"name"`
	Filings SubmissionFilings `json:"filings"`
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

type Filing struct {
	CIK             string
	Accession       string
	Form            string
	FilingDate      string
	ReportDate      string
	PrimaryDocument string
	DocumentTitle   string
}

func (s *CompanySubmissions) RecentFilings(cik string) []Filing {
	if s == nil {
		return nil
	}
	return filingsFromHistory(cik, s.Filings.Recent)
}

// AllFilings returns the recent filing history and any supplementary history
// files fetched by Client.Submissions.
func (s *CompanySubmissions) AllFilings(cik string) []Filing {
	if s == nil {
		return nil
	}
	histories := make([]FilingHistory, 0, 1+len(s.Filings.Supplemental))
	histories = append(histories, s.Filings.Recent)
	histories = append(histories, s.Filings.Supplemental...)
	seen := make(map[string]struct{})
	filings := make([]Filing, 0)
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

func filingsFromHistory(cik string, h FilingHistory) []Filing {
	count := len(h.AccessionNumber)
	for _, n := range []int{len(h.FilingDate), len(h.Form), len(h.PrimaryDocument)} {
		if n < count {
			count = n
		}
	}
	filings := make([]Filing, 0, count)
	for i := 0; i < count; i++ {
		accession := strings.TrimSpace(h.AccessionNumber[i])
		primaryDocument := strings.TrimSpace(h.PrimaryDocument[i])
		if accession == "" || primaryDocument == "" || strings.Contains(primaryDocument, "..") || strings.ContainsAny(primaryDocument, "/\\") {
			continue
		}
		filing := Filing{
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

type FinancialFact struct {
	Company    string  `json:"company"`
	CIK        string  `json:"cik"`
	Ticker     string  `json:"ticker"`
	Taxonomy   string  `json:"taxonomy"`
	Concept    string  `json:"concept"`
	Label      string  `json:"label"`
	Unit       string  `json:"unit"`
	Value      float64 `json:"value"`
	Start      string  `json:"period_start,omitempty"`
	End        string  `json:"period_end"`
	FilingDate string  `json:"filing_date"`
	Form       string  `json:"form"`
	Accession  string  `json:"accession_number"`
	Frame      string  `json:"frame,omitempty"`
}

var validCIK = regexp.MustCompile(`^[0-9]{1,10}$`)

func NormaliseCIK(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !validCIK.MatchString(raw) {
		return "", fmt.Errorf("invalid CIK %q", raw)
	}
	return strings.Repeat("0", 10-len(raw)) + raw, nil
}

var concepts = []string{
	"RevenueFromContractWithCustomerExcludingAssessedTax",
	"Revenues",
	"SalesRevenueNet",
	"AssetsCurrent",
	"LiabilitiesCurrent",
	"CashAndCashEquivalentsAtCarryingValue",
	"ShortTermBorrowings",
	"LongTermDebtCurrent",
	"LongTermDebtNoncurrent",
	"LongTermDebt",
	"OperatingIncomeLoss",
	"IncomeTaxExpenseBenefit",
	"InterestExpenseNonOperating",
	"InterestAndDebtExpense",
	"IncomeLossFromContinuingOperationsBeforeIncomeTaxesExtraordinaryItemsNoncontrollingInterest",
	"NetIncomeLoss",
	"DepreciationDepletionAndAmortization",
	"PaymentsToAcquirePropertyPlantAndEquipment",
	"NetCashProvidedByUsedInOperatingActivities",
	"StockholdersEquity",
	"EntityCommonStockSharesOutstanding",
	"WeightedAverageNumberOfDilutedSharesOutstanding",
}

var ifrsConcepts = []string{
	"Revenue",
	"CurrentAssets",
	"CurrentLiabilities",
	"CashAndCashEquivalents",
	"CurrentBorrowings",
	"CurrentBorrowingsAndCurrentPortionOfNoncurrentBorrowings",
	"CurrentPortionOfLongtermBorrowings",
	"Borrowings",
	"NoncurrentBorrowings",
	"ProfitLossFromOperatingActivities",
	"IncomeTaxExpenseContinuingOperations",
	"FinanceCosts",
	"ProfitLossBeforeTax",
	"ProfitLoss",
	"DepreciationAndAmortisationExpense",
	"PurchaseOfPropertyPlantAndEquipment",
	"PurchaseOfPropertyPlantAndEquipmentClassifiedAsInvestingActivities",
	"CashFlowsFromUsedInOperatingActivities",
	"Equity",
	"NumberOfSharesOutstanding",
	"DilutedWeightedAverageNumberOfOrdinarySharesOutstanding",
}

func ExtractFacts(data *CompanyFacts, cik string) []FinancialFact {
	var out []FinancialFact
	if data == nil {
		return out
	}

	for _, taxonomy := range []string{"us-gaap", "ifrs-full", "dei"} {
		definitions := data.Facts[taxonomy]
		taxonomyConcepts := concepts
		if taxonomy == "ifrs-full" {
			taxonomyConcepts = make([]string, 0, len(concepts)+len(ifrsConcepts))
			taxonomyConcepts = append(taxonomyConcepts, concepts...)
			taxonomyConcepts = append(taxonomyConcepts, ifrsConcepts...)
		}
		for _, concept := range taxonomyConcepts {
			def, ok := definitions[concept]
			if !ok {
				continue
			}

			for unit, entries := range def.Units {
				for _, e := range entries {
					if !secFilingForm(e.Form) {
						continue
					}
					out = append(out, FinancialFact{
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

func secFilingForm(form string) bool {
	form = strings.TrimSuffix(form, "/A")
	switch form {
	case "10-K", "10-Q", "20-F", "40-F", "6-K":
		return true
	default:
		return false
	}
}
