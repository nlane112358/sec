package fundamentals

type FinancialSummary struct {
	Company string
	Ticker  string
	Periods []SummaryPeriod
	Metrics []SummaryMetric
}

type SummaryPeriod struct {
	Year      string `json:"Year"`
	End       string `json:"End"`
	Quarter   string `json:"Quarter,omitempty"`
	Label     string `json:"Label,omitempty"`
	Form      string `json:"Form,omitempty"`
	StartDate string `json:"StartDate,omitempty"`
}

type SummaryMetric struct {
	Name   string         `json:"Name"`
	Values []SummaryValue `json:"Values"`
}

type SummaryValue struct {
	Value       float64 `json:"Value"`
	Unit        string  `json:"Unit"`
	Present     bool    `json:"Present"`
	Taxonomy    string  `json:"Taxonomy,omitempty"`
	Concept     string  `json:"Concept,omitempty"`
	Label       string  `json:"Label,omitempty"`
	Form        string  `json:"Form,omitempty"`
	FilingDate  string  `json:"FilingDate,omitempty"`
	Accession   string  `json:"Accession,omitempty"`
	PeriodEnd   string  `json:"PeriodEnd,omitempty"`
	Formula     string  `json:"Formula,omitempty"`
	Calculation string  `json:"Calculation,omitempty"`
	DocumentURL string  `json:"DocumentURL,omitempty"`
}

type IntangiblesResult struct {
	Periods               []SummaryPeriod `json:"Periods"`
	Currency              string          `json:"Currency"`
	IntangibleCapital     []SummaryValue  `json:"IntangibleCapital"`
	IntangibleAmort       []SummaryValue  `json:"IntangibleAmort"`
	AdjustedEBIT          []SummaryValue  `json:"AdjustedEBIT"`
	AdjustedInvestedCap   []SummaryValue  `json:"AdjustedInvestedCap"`
	AdjustedROIC          []SummaryValue  `json:"AdjustedROIC"`
	AdjustedOwnerEarnings []SummaryValue  `json:"AdjustedOwnerEarnings"`
}

type QuarterlyInflection struct {
	Metric                string  `json:"metric"`
	PeriodLabel           string  `json:"period_label,omitempty"`
	LatestValue           float64 `json:"latest_value"`
	PriorQuarterValue     float64 `json:"prior_quarter_value"`
	PriorYearQuarterValue float64 `json:"prior_year_quarter_value"`
	QoQGrowth             float64 `json:"qoq_growth"`
	YoYGrowth             float64 `json:"yoy_growth"`
	PriorYoYGrowth        float64 `json:"prior_yoy_growth"`
	IsAccelerating        bool    `json:"is_accelerating"`
}

type TTMSummary struct {
	Company     string                `json:"company"`
	Ticker      string                `json:"ticker"`
	TTMPeriod   SummaryPeriod         `json:"ttm_period"`
	Metrics     []SummaryMetric       `json:"metrics"`
	Intangibles IntangiblesResult     `json:"intangibles"`
	Inflections []QuarterlyInflection `json:"inflections"`
}

type SummaryView string

const (
	ViewAnnual    SummaryView = "annual"
	ViewQuarterly SummaryView = "quarterly"
	ViewTTM       SummaryView = "ttm"
)
