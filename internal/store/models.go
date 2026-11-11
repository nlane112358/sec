package store

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

type Filing struct {
	CIK             string
	Accession       string
	Form            string
	FilingDate      string
	ReportDate      string
	PrimaryDocument string
	DocumentTitle   string
}

type LocalCompany struct {
	CIK            string
	Name           string
	Ticker         string
	SIC            string
	SICDescription string
}
