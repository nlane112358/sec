package fundamentals

type FinancialSummary struct {
	Company string
	Ticker  string
	Periods []SummaryPeriod
	Metrics []SummaryMetric
}

type SummaryPeriod struct {
	Year string
	End  string
}

type SummaryMetric struct {
	Name   string
	Values []SummaryValue
}

type SummaryValue struct {
	Value   float64
	Unit    string
	Present bool
}

type IntangiblesResult struct {
	Periods               []SummaryPeriod
	Currency              string
	IntangibleCapital     []SummaryValue
	IntangibleAmort       []SummaryValue
	AdjustedEBIT          []SummaryValue
	AdjustedInvestedCap   []SummaryValue
	AdjustedROIC          []SummaryValue
	AdjustedOwnerEarnings []SummaryValue
}
