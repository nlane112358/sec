package sec

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

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

type summaryFact struct {
	taxonomy  string
	concept   string
	unit      string
	value     float64
	start     string
	end       string
	filed     string
	form      string
	accession string
}

type summaryConcept struct {
	taxonomy        string
	concept         string
	duration        bool
	allowSameFYYear bool
}

type summaryMetricDefinition struct {
	name         string
	concepts     []summaryConcept
	freeCashFlow bool
}

var summaryMetricDefinitions = []summaryMetricDefinition{
	{
		name: "Revenue",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "RevenueFromContractWithCustomerExcludingAssessedTax", duration: true},
			{taxonomy: "us-gaap", concept: "Revenues", duration: true},
			{taxonomy: "us-gaap", concept: "SalesRevenueNet", duration: true},
			{taxonomy: "ifrs-full", concept: "Revenue", duration: true},
		},
	},
	{
		name: "Operating Income",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "OperatingIncomeLoss", duration: true},
			{taxonomy: "ifrs-full", concept: "ProfitLossFromOperatingActivities", duration: true},
		},
	},
	{
		name: "Net Income",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "NetIncomeLoss", duration: true},
			{taxonomy: "ifrs-full", concept: "ProfitLoss", duration: true},
		},
	},
	{
		name: "Operating Cash Flow",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "NetCashProvidedByUsedInOperatingActivities", duration: true},
			{taxonomy: "ifrs-full", concept: "CashFlowsFromUsedInOperatingActivities", duration: true},
		},
	},
	{
		name: "Capital Expenditure",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "PaymentsToAcquirePropertyPlantAndEquipment", duration: true},
			{taxonomy: "ifrs-full", concept: "PurchaseOfPropertyPlantAndEquipment", duration: true},
			{taxonomy: "ifrs-full", concept: "PurchaseOfPropertyPlantAndEquipmentClassifiedAsInvestingActivities", duration: true},
		},
	},
	{
		name:         "Free Cash Flow",
		freeCashFlow: true,
	},
	{
		name: "Cash & Equivalents",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "CashAndCashEquivalentsAtCarryingValue"},
			{taxonomy: "ifrs-full", concept: "CashAndCashEquivalents"},
		},
	},
	{
		name: "Debt / Borrowings",
	},
	{
		name: "Shares Outstanding",
		concepts: []summaryConcept{
			{taxonomy: "dei", concept: "EntityCommonStockSharesOutstanding", allowSameFYYear: true},
			{taxonomy: "ifrs-full", concept: "NumberOfSharesOutstanding", allowSameFYYear: true},
			{taxonomy: "us-gaap", concept: "WeightedAverageNumberOfDilutedSharesOutstanding", duration: true},
			{taxonomy: "ifrs-full", concept: "DilutedWeightedAverageNumberOfOrdinarySharesOutstanding", duration: true},
		},
	},
}

// FinancialSummary reads annual facts already stored for the company and
// returns the requested number of most recent fiscal years.
func (s *Store) FinancialSummary(ctx context.Context, yearCount int) (FinancialSummary, error) {
	if yearCount < 1 {
		yearCount = 5
	}
	if yearCount > 5 {
		yearCount = 5
	}
	var summary FinancialSummary
	var cik string
	if err := s.db.QueryRowContext(ctx, `SELECT cik, company_name, requested_ticker
FROM sec_companies LIMIT 1`).Scan(&cik, &summary.Company, &summary.Ticker); err != nil {
		return summary, fmt.Errorf("load company for summary: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT taxonomy, concept, unit, value,
    period_start, period_end, filing_date, form, accession_number
FROM sec_facts WHERE cik = ?`, cik)
	if err != nil {
		return summary, fmt.Errorf("load facts for summary: %w", err)
	}
	defer rows.Close()
	facts := make([]summaryFact, 0)
	for rows.Next() {
		var fact summaryFact
		if err := rows.Scan(&fact.taxonomy, &fact.concept, &fact.unit, &fact.value,
			&fact.start, &fact.end, &fact.filed, &fact.form, &fact.accession); err != nil {
			return summary, fmt.Errorf("read fact for summary: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("read facts for summary: %w", err)
	}
	if err := rows.Close(); err != nil {
		return summary, fmt.Errorf("close facts query: %w", err)
	}

	periodEnds := annualPeriodEnds(facts)
	if len(periodEnds) == 0 {
		periodEnds = instantPeriodEnds(facts)
	}
	if len(periodEnds) > yearCount {
		periodEnds = periodEnds[:yearCount]
	}
	summary.Periods = make([]SummaryPeriod, 0, len(periodEnds))
	for _, end := range periodEnds {
		summary.Periods = append(summary.Periods, SummaryPeriod{Year: end[:4], End: end})
	}

	for _, definition := range summaryMetricDefinitions {
		metric := SummaryMetric{Name: definition.name, Values: make([]SummaryValue, len(summary.Periods))}
		for i, period := range summary.Periods {
			switch {
			case definition.freeCashFlow:
				cashFlow := summaryMetricValue(summary.Metrics, "Operating Cash Flow", i)
				capEx := summaryMetricValue(summary.Metrics, "Capital Expenditure", i)
				if cashFlow.Present && capEx.Present && cashFlow.Unit == capEx.Unit {
					metric.Values[i] = SummaryValue{
						Value:   cashFlow.Value - math.Abs(capEx.Value),
						Unit:    cashFlow.Unit,
						Present: true,
					}
				}
			case definition.name == "Debt / Borrowings":
				metric.Values[i] = selectDebt(facts, period.End)
			default:
				metric.Values[i], _ = selectSummaryFact(facts, definition.concepts, period.End)
			}
		}
		summary.Metrics = append(summary.Metrics, metric)
	}
	return summary, nil
}

func annualPeriodEnds(facts []summaryFact) []string {
	byYear := make(map[string]string)
	for _, fact := range facts {
		if fact.start == "" || !isAnnualForm(fact.form) || !isAnnualDuration(fact.start, fact.end) {
			continue
		}
		year := fact.end[:4]
		if fact.end > byYear[year] {
			byYear[year] = fact.end
		}
	}
	return sortedPeriodEnds(byYear)
}

func instantPeriodEnds(facts []summaryFact) []string {
	byYear := make(map[string]string)
	for _, fact := range facts {
		if fact.start != "" || !isAnnualForm(fact.form) || len(fact.end) < 4 {
			continue
		}
		year := fact.end[:4]
		if fact.end > byYear[year] {
			byYear[year] = fact.end
		}
	}
	return sortedPeriodEnds(byYear)
}

func sortedPeriodEnds(byYear map[string]string) []string {
	ends := make([]string, 0, len(byYear))
	for _, end := range byYear {
		ends = append(ends, end)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ends)))
	return ends
}

func isAnnualForm(form string) bool {
	form = strings.TrimSuffix(form, "/A")
	switch form {
	case "10-K", "20-F", "40-F", "6-K":
		return true
	default:
		return false
	}
}

func isAnnualDuration(start, end string) bool {
	startDate, startErr := time.Parse("2006-01-02", start)
	endDate, endErr := time.Parse("2006-01-02", end)
	if startErr != nil || endErr != nil || endDate.Before(startDate) {
		return false
	}
	days := int(endDate.Sub(startDate).Hours()/24) + 1
	return days >= 300 && days <= 400
}

func selectSummaryFact(facts []summaryFact, concepts []summaryConcept, periodEnd string) (SummaryValue, bool) {
	for _, concept := range concepts {
		var matches []summaryFact
		for _, fact := range facts {
			if fact.taxonomy != concept.taxonomy || fact.concept != concept.concept || !isAnnualForm(fact.form) {
				continue
			}
			if concept.duration {
				if fact.start == "" || fact.end != periodEnd || !isAnnualDuration(fact.start, fact.end) {
					continue
				}
			} else if fact.start != "" || !matchingInstantPeriod(fact.end, periodEnd, concept.allowSameFYYear) {
				continue
			}
			matches = append(matches, fact)
		}
		if len(matches) == 0 {
			continue
		}
		best := matches[0]
		for _, candidate := range matches[1:] {
			if betterSummaryFact(candidate, best, periodEnd) {
				best = candidate
			}
		}
		return SummaryValue{Value: best.value, Unit: best.unit, Present: true}, true
	}
	return SummaryValue{}, false
}

func matchingInstantPeriod(factEnd, periodEnd string, allowSameFYYear bool) bool {
	if factEnd == periodEnd {
		return true
	}
	return allowSameFYYear && len(factEnd) >= 4 && len(periodEnd) >= 4 && factEnd[:4] == periodEnd[:4]
}

func betterSummaryFact(candidate, current summaryFact, periodEnd string) bool {
	candidateDistance := periodDistance(candidate.end, periodEnd)
	currentDistance := periodDistance(current.end, periodEnd)
	if candidateDistance != currentDistance {
		return candidateDistance < currentDistance
	}
	candidateUnitRank := summaryUnitRank(candidate.unit)
	currentUnitRank := summaryUnitRank(current.unit)
	if candidateUnitRank != currentUnitRank {
		return candidateUnitRank < currentUnitRank
	}
	if candidate.filed != current.filed {
		return candidate.filed > current.filed
	}
	return candidate.accession > current.accession
}

func periodDistance(a, b string) int64 {
	dateA, errA := time.Parse("2006-01-02", a)
	dateB, errB := time.Parse("2006-01-02", b)
	if errA != nil || errB != nil {
		return int64(1<<63 - 1)
	}
	days := int64(dateA.Sub(dateB).Hours() / 24)
	if days < 0 {
		return -days
	}
	return days
}

func summaryUnitRank(unit string) int {
	if strings.EqualFold(unit, "USD") || strings.EqualFold(unit, "shares") {
		return 0
	}
	return 1
}

func selectDebt(facts []summaryFact, periodEnd string) SummaryValue {
	if value, ok := selectSummaryFact(facts, []summaryConcept{
		{taxonomy: "ifrs-full", concept: "Borrowings"},
	}, periodEnd); ok {
		return value
	}
	if value, ok := selectSummaryFact(facts, []summaryConcept{
		{taxonomy: "us-gaap", concept: "LongTermDebtNoncurrent"},
		{taxonomy: "us-gaap", concept: "LongTermDebt"},
	}, periodEnd); ok {
		return addDebtComponents(value, facts, periodEnd, []summaryConcept{
			{taxonomy: "us-gaap", concept: "LongTermDebtCurrent"},
			{taxonomy: "us-gaap", concept: "ShortTermBorrowings"},
		})
	}
	if value, ok := selectSummaryFact(facts, []summaryConcept{
		{taxonomy: "ifrs-full", concept: "NoncurrentBorrowings"},
	}, periodEnd); ok {
		current, currentOK := selectSummaryFact(facts, []summaryConcept{
			{taxonomy: "ifrs-full", concept: "CurrentBorrowingsAndCurrentPortionOfNoncurrentBorrowings"},
			{taxonomy: "ifrs-full", concept: "CurrentPortionOfLongtermBorrowings"},
			{taxonomy: "ifrs-full", concept: "CurrentBorrowings"},
		}, periodEnd)
		if currentOK && current.Unit == value.Unit {
			value.Value += current.Value
		}
		return value
	}
	if current, ok := selectSummaryFact(facts, []summaryConcept{
		{taxonomy: "ifrs-full", concept: "CurrentBorrowingsAndCurrentPortionOfNoncurrentBorrowings"},
		{taxonomy: "ifrs-full", concept: "CurrentPortionOfLongtermBorrowings"},
		{taxonomy: "ifrs-full", concept: "CurrentBorrowings"},
	}, periodEnd); ok {
		return current
	}
	longTermCurrent, hasLongTermCurrent := selectSummaryFact(facts, []summaryConcept{
		{taxonomy: "us-gaap", concept: "LongTermDebtCurrent"},
	}, periodEnd)
	shortTerm, hasShortTerm := selectSummaryFact(facts, []summaryConcept{
		{taxonomy: "us-gaap", concept: "ShortTermBorrowings"},
	}, periodEnd)
	if hasLongTermCurrent && hasShortTerm && longTermCurrent.Unit == shortTerm.Unit {
		longTermCurrent.Value += shortTerm.Value
		return longTermCurrent
	}
	if hasLongTermCurrent {
		return longTermCurrent
	}
	if hasShortTerm {
		return shortTerm
	}
	return SummaryValue{}
}

func addDebtComponents(total SummaryValue, facts []summaryFact, periodEnd string, components []summaryConcept) SummaryValue {
	for _, component := range components {
		value, ok := selectSummaryFact(facts, []summaryConcept{component}, periodEnd)
		if !ok || value.Unit != total.Unit {
			continue
		}
		total.Value += value.Value
	}
	return total
}

func summaryMetricValue(metrics []SummaryMetric, name string, index int) SummaryValue {
	for _, metric := range metrics {
		if metric.Name == name && index < len(metric.Values) {
			return metric.Values[index]
		}
	}
	return SummaryValue{}
}

func WriteFinancialSummary(output io.Writer, summary FinancialSummary) error {
	if _, err := fmt.Fprintf(output, "%s (%s) — annual facts\n", summary.Company, summary.Ticker); err != nil {
		return err
	}
	if len(summary.Periods) == 0 {
		_, err := fmt.Fprintln(output, "No annual facts are saved yet. Refresh this company first.")
		return err
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprint(writer, "Metric"); err != nil {
		return err
	}
	for _, period := range summary.Periods {
		if _, err := fmt.Fprintf(writer, "\tFY %s", period.Year); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	if _, err := fmt.Fprint(writer, "Period End"); err != nil {
		return err
	}
	for _, period := range summary.Periods {
		if _, err := fmt.Fprintf(writer, "\t%s", period.End); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	for _, metric := range summary.Metrics {
		if _, err := fmt.Fprint(writer, metric.Name); err != nil {
			return err
		}
		for _, value := range metric.Values {
			cell := "—"
			if value.Present {
				number := value.Value
				if metric.Name == "Capital Expenditure" {
					number = math.Abs(number)
				}
				cell = formatSummaryValue(number, value.Unit)
			}
			if _, err := fmt.Fprintf(writer, "\t%s", cell); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(output, "Free cash flow = operating cash flow − capital expenditure. Shares use period-end outstanding when available; otherwise diluted weighted-average shares.")
	return err
}

func formatSummaryValue(value float64, unit string) string {
	magnitude := math.Abs(value)
	divisor := 1.0
	suffix := ""
	switch {
	case magnitude >= 1e12:
		divisor, suffix = 1e12, "T"
	case magnitude >= 1e9:
		divisor, suffix = 1e9, "B"
	case magnitude >= 1e6:
		divisor, suffix = 1e6, "M"
	case magnitude >= 1e3:
		divisor, suffix = 1e3, "K"
	}
	formatted := strconv.FormatFloat(value/divisor, 'f', 2, 64) + suffix
	if unit != "" {
		formatted += " " + unit
	}
	return formatted
}
