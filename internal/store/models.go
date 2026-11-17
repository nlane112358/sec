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

// CompanySummaryRecord persists a company's computed annual summary JSON
// alongside filing/share metadata for O(1) incremental freshness checks.
type CompanySummaryRecord struct {
	CIK                        string
	Ticker                     string
	CompanyName                string
	SIC                        string
	Currency                   string
	LatestPeriodEnd            string
	PeriodsCount               int
	LatestFilingDate           string
	LatestAccession            string
	LatestFinancialFilingDate  string
	FilingsCount               int
	LatestShareCount           float64
	HasLatestShareCount        bool
	SummaryJSON                string
	SchemaVersion              int
}

// RankVariableRecord persists all intrinsic per-company rank factors and the
// full-universe score/peer_group for a specific (cik, years) window.
type RankVariableRecord struct {
	CIK                       string
	Years                     int
	Ticker                    string
	CompanyName               string
	SIC                       string
	Sector                    string
	RawPeerGroup              string
	PeerGroup                 string
	Score                     float64
	Eligible                  bool
	LatestFinancialFilingDate string
	Year                      string
	TrendYears                int
	Revenue                   float64
	Currency                  string
	ROIC                      float64
	HasROIC                   bool
	ROE                       float64
	HasROE                    bool
	OperatingMargin           float64
	OperatingConsistency      float64
	HasOperatingMargin        bool
	FCFMargin                 float64
	OwnerEarningsMargin       float64
	HasOwnerEarnings          bool
	HasSBCAdjustment          bool
	RevenueCAGR               float64
	HasRevenueGrowth          bool
	EPSGrowth                 float64
	HasEPSGrowth              bool
	BalanceSheetQuality       float64
	HasBalanceSheet           bool
	BookValueGrowth           float64
	HasBookValueGrowth        bool
	ShareReduction            float64
	HasShareTrend             bool
	SplitAdjusted             bool
	SchemaVersion             int
}

// GAAPArbitrageVariableRecord persists all intrinsic Peters & Taylor / Damodaran
// intangible-capitalized and GAAP comparison variables for (cik, years).
type GAAPArbitrageVariableRecord struct {
	CIK                       string
	Years                     int
	Ticker                    string
	CompanyName               string
	SIC                       string
	SICDescription            string
	Sector                    string
	Currency                  string
	Eligible                  bool
	LatestFinancialFilingDate string
	GAAPRevenue               float64
	GAAPNetIncome             float64
	GAAPOperatingIncome       float64
	GAAPOperatingMargin       float64
	GAAPROIC                  float64
	HasGAAPROIC               bool
	GAAPUnprofitable          bool
	IntangibleCapital         float64
	AdjustedEBIT              float64
	AdjustedEBITMargin        float64
	AdjustedInvestedCap       float64
	AdjustedROIC              float64
	HasAdjustedROIC           bool
	AvgAdjustedROIC           float64
	ROICDelta                 float64
	SurgingROIC               bool
	OwnerEarnings             float64
	HasOwnerEarnings          bool
	AdjustedOwnerEarnings     float64
	SharesOutstanding         float64
	BaseArbitrageScore        float64
	BaseArbitrageSignal       string
	SchemaVersion             int
}

// CompanyPrecomputedBundle groups all derived records written for one CIK in a
// single SQLite transaction.
type CompanyPrecomputedBundle struct {
	Summary  *CompanySummaryRecord
	RankVars []RankVariableRecord
	ArbVars  []GAAPArbitrageVariableRecord
}

// RankScoreUpdate updates the full-universe peer-group score for one (cik, years).
type RankScoreUpdate struct {
	CIK           string
	Years         int
	PeerGroup     string
	Score         float64
	SchemaVersion int
}

// PrecomputeStats summarizes coverage and freshness of saved precomputed tables.
type PrecomputeStats struct {
	TotalCompanies   int
	CleanSummaries   int
	DirtySummaries   int
	MissingSummaries int
	Rank5Count       int
	Rank6Count       int
	Arb5Count        int
}
