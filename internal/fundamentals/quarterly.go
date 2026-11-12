package fundamentals

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

	"sec/internal/store"
)

// isQuarterlyDuration reports whether start and end span roughly one quarter (65 to 125 days).
func isQuarterlyDuration(start, end string) bool {
	days := durationDays(start, end)
	return days >= 65 && days <= 125
}

// isHalfYearDuration reports whether start and end span roughly two quarters (150 to 215 days).
func isHalfYearDuration(start, end string) bool {
	days := durationDays(start, end)
	return days >= 150 && days <= 215
}

// isNineMonthDuration reports whether start and end span roughly three quarters (240 to 305 days).
func isNineMonthDuration(start, end string) bool {
	days := durationDays(start, end)
	return days >= 240 && days <= 305
}

func durationDays(start, end string) int {
	startDate, err1 := time.Parse("2006-01-02", start)
	endDate, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil || endDate.Before(startDate) {
		return 0
	}
	return int(endDate.Sub(startDate).Hours()/24) + 1
}

// quarterlyPeriodEnds extracts sorted quarterly period ends from duration facts.
func quarterlyPeriodEnds(facts []summaryFact) []SummaryPeriod {
	type rawPeriod struct {
		end   string
		start string
		isFY  bool
		date  time.Time
	}

	byEnd := make(map[string]rawPeriod)

	for _, f := range facts {
		if f.start == "" || !secFilingForm(f.form) {
			continue
		}
		endDate, err := time.Parse("2006-01-02", f.end)
		if err != nil {
			continue
		}

		days := durationDays(f.start, f.end)

		// 1. Discrete 10-Q quarter (65 to 125 days)
		if days >= 65 && days <= 125 {
			if existing, ok := byEnd[f.end]; !ok || existing.start == "" {
				byEnd[f.end] = rawPeriod{end: f.end, start: f.start, isFY: false, date: endDate}
			}
		}

		// 2. Annual 10-K / 20-F FY end (300 to 400 days) which represents Q4 / FY end
		if days >= 300 && days <= 400 && isAnnualForm(f.form) {
			if existing, ok := byEnd[f.end]; !ok || !existing.isFY {
				byEnd[f.end] = rawPeriod{end: f.end, start: f.start, isFY: true, date: endDate}
			}
		}
	}

	var list []rawPeriod
	for _, p := range byEnd {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].end > list[j].end
	})

	var deduped []rawPeriod
	for _, p := range list {
		tooClose := false
		for _, kept := range deduped {
			if periodDistance(p.end, kept.end) < 45 {
				tooClose = true
				break
			}
		}
		if !tooClose {
			deduped = append(deduped, p)
		}
	}

	var periods []SummaryPeriod
	for _, p := range deduped {
		month := int(p.date.Month())
		quarterNum := (month-1)/3 + 1
		quarterStr := fmt.Sprintf("Q%d", quarterNum)
		yearStr := strconv.Itoa(p.date.Year())
		label := fmt.Sprintf("%s %s", quarterStr, yearStr)

		formStr := "10-Q"
		if p.isFY {
			formStr = "10-K"
		}

		periods = append(periods, SummaryPeriod{
			Year:      yearStr,
			End:       p.end,
			Quarter:   quarterStr,
			Label:     label,
			Form:      formStr,
			StartDate: p.start,
		})
	}

	return periods
}

// selectQuarterlyFact finds or computes the discrete fact value for a given quarterly period.
func selectQuarterlyFact(facts []summaryFact, concepts []summaryConcept, period SummaryPeriod, preferredCurrency ...string) (SummaryValue, bool) {
	// 1. Instant facts (Balance sheet: Cash, Debt, Equity, Assets, Liabilities, Shares)
	if len(concepts) > 0 && !concepts[0].duration {
		for _, concept := range concepts {
			var matches []summaryFact
			for _, fact := range facts {
				if !matchesConceptTaxonomy(fact.taxonomy, concept.taxonomy) || fact.concept != concept.concept ||
					!secFilingForm(fact.form) || fact.start != "" {
					continue
				}
				if periodDistance(fact.end, period.End) <= 25 && finite(fact.value) {
					matches = append(matches, fact)
				}
			}
			if len(matches) > 0 {
				best := matches[0]
				for _, candidate := range matches[1:] {
					if betterQuarterlyFact(candidate, best, period.End, preferredCurrency...) {
						best = candidate
					}
				}
				return SummaryValue{Value: best.value, Unit: best.unit, Present: true}, true
			}
		}
		return SummaryValue{}, false
	}

	// 2. Duration facts (Income Statement & Cash Flow):
	// A) Try finding a direct discrete quarterly fact (65-125 days)
	for _, concept := range concepts {
		var matches []summaryFact
		for _, fact := range facts {
			if !matchesConceptTaxonomy(fact.taxonomy, concept.taxonomy) || fact.concept != concept.concept ||
				!secFilingForm(fact.form) || fact.start == "" {
				continue
			}
			if periodDistance(fact.end, period.End) <= 14 && isQuarterlyDuration(fact.start, fact.end) && finite(fact.value) {
				matches = append(matches, fact)
			}
		}
		if len(matches) > 0 {
			best := matches[0]
			for _, candidate := range matches[1:] {
				if betterQuarterlyFact(candidate, best, period.End, preferredCurrency...) {
					best = candidate
				}
			}
			return SummaryValue{Value: best.value, Unit: best.unit, Present: true}, true
		}
	}

	// B) If period is Annual FY End (Q4): Derive Q4 = Annual (12M) - YTD Q3 (9M)
	if period.Form == "10-K" {
		for _, concept := range concepts {
			for _, annFact := range facts {
				if !matchesConceptTaxonomy(annFact.taxonomy, concept.taxonomy) || annFact.concept != concept.concept ||
					annFact.start == "" || periodDistance(annFact.end, period.End) > 14 || !isAnnualDuration(annFact.start, annFact.end) || !finite(annFact.value) {
					continue
				}
				// Find matching 9-month YTD fact starting near annFact.start
				for _, ytdFact := range facts {
					if !matchesConceptTaxonomy(ytdFact.taxonomy, concept.taxonomy) || ytdFact.concept != concept.concept ||
						ytdFact.start == "" || periodDistance(ytdFact.start, annFact.start) > 14 || !isNineMonthDuration(ytdFact.start, ytdFact.end) || !finite(ytdFact.value) {
						continue
					}
					if strings.EqualFold(annFact.unit, ytdFact.unit) {
						diff := annFact.value - ytdFact.value
						return SummaryValue{Value: diff, Unit: annFact.unit, Present: true}, true
					}
				}
			}
		}
	}

	// C) If period is Q3: Derive Q3 = YTD 9M - YTD 6M
	for _, concept := range concepts {
		for _, ytd9Fact := range facts {
			if !matchesConceptTaxonomy(ytd9Fact.taxonomy, concept.taxonomy) || ytd9Fact.concept != concept.concept ||
				ytd9Fact.start == "" || periodDistance(ytd9Fact.end, period.End) > 14 || !isNineMonthDuration(ytd9Fact.start, ytd9Fact.end) || !finite(ytd9Fact.value) {
				continue
			}
			for _, ytd6Fact := range facts {
				if !matchesConceptTaxonomy(ytd6Fact.taxonomy, concept.taxonomy) || ytd6Fact.concept != concept.concept ||
					ytd6Fact.start == "" || periodDistance(ytd6Fact.start, ytd9Fact.start) > 14 || !isHalfYearDuration(ytd6Fact.start, ytd6Fact.end) || !finite(ytd6Fact.value) {
					continue
				}
				if strings.EqualFold(ytd9Fact.unit, ytd6Fact.unit) {
					diff := ytd9Fact.value - ytd6Fact.value
					return SummaryValue{Value: diff, Unit: ytd9Fact.unit, Present: true}, true
				}
			}
		}
	}

	// D) If period is Q2: Derive Q2 = YTD 6M - Q1 3M
	for _, concept := range concepts {
		for _, ytd6Fact := range facts {
			if !matchesConceptTaxonomy(ytd6Fact.taxonomy, concept.taxonomy) || ytd6Fact.concept != concept.concept ||
				ytd6Fact.start == "" || periodDistance(ytd6Fact.end, period.End) > 14 || !isHalfYearDuration(ytd6Fact.start, ytd6Fact.end) || !finite(ytd6Fact.value) {
				continue
			}
			for _, q1Fact := range facts {
				if !matchesConceptTaxonomy(q1Fact.taxonomy, concept.taxonomy) || q1Fact.concept != concept.concept ||
					q1Fact.start == "" || periodDistance(q1Fact.start, ytd6Fact.start) > 14 || !isQuarterlyDuration(q1Fact.start, q1Fact.end) || !finite(q1Fact.value) {
					continue
				}
				if strings.EqualFold(ytd6Fact.unit, q1Fact.unit) {
					diff := ytd6Fact.value - q1Fact.value
					return SummaryValue{Value: diff, Unit: ytd6Fact.unit, Present: true}, true
				}
			}
		}
	}

	return SummaryValue{}, false
}

func betterQuarterlyFact(candidate, current summaryFact, periodEnd string, preferredCurrency ...string) bool {
	candDist := periodDistance(candidate.end, periodEnd)
	currDist := periodDistance(current.end, periodEnd)
	if candDist != currDist {
		return candDist < currDist
	}
	candRank := summaryUnitRank(candidate.unit, preferredCurrency...)
	currRank := summaryUnitRank(current.unit, preferredCurrency...)
	if candRank != currRank {
		return candRank < currRank
	}
	return candidate.filed > current.filed
}

// LoadQuarterlySummary loads the last N quarters of financial metrics.
func LoadQuarterlySummary(ctx context.Context, s *store.Store, quarterCount int, ticker ...string) (FinancialSummary, error) {
	if quarterCount < 1 {
		quarterCount = 8
	}
	if quarterCount > 16 {
		quarterCount = 16
	}

	var summary FinancialSummary
	var cik string
	var err error
	if len(ticker) > 0 && strings.TrimSpace(ticker[0]) != "" {
		cik, summary.Company, summary.Ticker, err = s.CompanyForTicker(ctx, ticker[0])
	} else {
		err = s.DB().QueryRowContext(ctx, `SELECT c.cik, c.company_name, t.ticker
FROM sec_tickers t JOIN sec_companies c ON c.cik = t.cik
ORDER BY t.ticker LIMIT 1`).Scan(&cik, &summary.Company, &summary.Ticker)
	}
	if err != nil {
		return summary, fmt.Errorf("load company for quarterly summary: %w", err)
	}

	rows, err := s.DB().QueryContext(ctx, `SELECT taxonomy, concept, unit, value,
    period_start, period_end, filing_date, form, accession_number
FROM sec_facts WHERE cik = ?`, cik)
	if err != nil {
		return summary, fmt.Errorf("load facts for quarterly summary: %w", err)
	}
	defer rows.Close()

	var facts []summaryFact
	for rows.Next() {
		var fact summaryFact
		if err := rows.Scan(&fact.taxonomy, &fact.concept, &fact.unit, &fact.value,
			&fact.start, &fact.end, &fact.filed, &fact.form, &fact.accession); err != nil {
			return summary, fmt.Errorf("read fact: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("read facts: %w", err)
	}

	allPeriods := quarterlyPeriodEnds(facts)
	if len(allPeriods) == 0 {
		return LoadFinancialSummary(ctx, s, 5, ticker...)
	}

	if len(allPeriods) > quarterCount {
		summary.Periods = allPeriods[:quarterCount]
	} else {
		summary.Periods = allPeriods
	}

	primaryCurrency := detectPrimaryCurrency(facts)

	for _, definition := range summaryMetricDefinitions {
		metric := SummaryMetric{Name: definition.name, Values: make([]SummaryValue, len(summary.Periods))}
		for i, period := range summary.Periods {
			switch {
			case definition.freeCashFlow:
				cashFlow := summaryMetricValue(summary.Metrics, "Operating Cash Flow", i)
				capEx := summaryMetricValue(summary.Metrics, "Capital Expenditure", i)
				if cashFlow.Present && capEx.Present && strings.EqualFold(cashFlow.Unit, capEx.Unit) {
					metric.Values[i] = SummaryValue{
						Value:   cashFlow.Value - math.Abs(capEx.Value),
						Unit:    cashFlow.Unit,
						Present: true,
					}
				} else if cashFlow.Present && !capEx.Present {
					metric.Values[i] = SummaryValue{
						Value:   cashFlow.Value,
						Unit:    cashFlow.Unit,
						Present: true,
					}
				}
			case definition.ownerEarnings:
				freeCashFlow := summaryMetricValue(summary.Metrics, "Free Cash Flow", i)
				sbc := summaryMetricValue(summary.Metrics, "Stock-Based Compensation", i)
				if freeCashFlow.Present && sbc.Present && strings.EqualFold(freeCashFlow.Unit, sbc.Unit) {
					metric.Values[i] = SummaryValue{
						Value:   freeCashFlow.Value - math.Abs(sbc.Value),
						Unit:    freeCashFlow.Unit,
						Present: true,
					}
				}
			case definition.name == "Capital Expenditure":
				metric.Values[i], _ = selectQuarterlyFact(facts, definition.concepts, period, primaryCurrency)
				if !metric.Values[i].Present {
					cashFlow := summaryMetricValue(summary.Metrics, "Operating Cash Flow", i)
					if cashFlow.Present {
						metric.Values[i] = SummaryValue{
							Value:   0.0,
							Unit:    cashFlow.Unit,
							Present: true,
						}
					}
				}
			case definition.sgaExpense:
				metric.Values[i] = selectQuarterlySGA(facts, period, definition.concepts, primaryCurrency)
			case definition.name == "Debt / Borrowings":
				metric.Values[i] = selectDebt(facts, period.End, primaryCurrency)
			case definition.intangibleAmortization || definition.adjustedOperatingIncome ||
				definition.intangibleCapital || definition.adjustedInvestedCapital || definition.adjustedROIC:
				// Derived below
			default:
				metric.Values[i], _ = selectQuarterlyFact(facts, definition.concepts, period, primaryCurrency)
			}
		}
		summary.Metrics = append(summary.Metrics, metric)
	}

	deriveOperatingIncome(&summary)
	sanitizeSharesOutstanding(&summary)
	deriveQuarterlyIntangibles(&summary, s, facts)

	return summary, nil
}

func selectQuarterlySGA(facts []summaryFact, period SummaryPeriod, concepts []summaryConcept, preferredCurrency ...string) SummaryValue {
	if val, ok := selectQuarterlyFact(facts, concepts, period, preferredCurrency...); ok {
		return val
	}
	smConcepts := []summaryConcept{
		{taxonomy: "us-gaap", concept: "SellingAndMarketingExpense", duration: true},
		{taxonomy: "us-gaap", concept: "MarketingExpense", duration: true},
		{taxonomy: "us-gaap", concept: "MarketingAndAdvertisingExpense", duration: true},
		{taxonomy: "us-gaap", concept: "SellingExpense", duration: true},
		{taxonomy: "ifrs-full", concept: "DistributionCosts", duration: true},
	}
	gaConcepts := []summaryConcept{
		{taxonomy: "us-gaap", concept: "GeneralAndAdministrativeExpense", duration: true},
		{taxonomy: "us-gaap", concept: "OtherGeneralAndAdministrativeExpense", duration: true},
		{taxonomy: "us-gaap", concept: "GeneralExpense", duration: true},
		{taxonomy: "ifrs-full", concept: "AdministrativeExpense", duration: true},
	}
	sm, okSM := selectQuarterlyFact(facts, smConcepts, period, preferredCurrency...)
	ga, okGA := selectQuarterlyFact(facts, gaConcepts, period, preferredCurrency...)
	if okSM && okGA && strings.EqualFold(sm.Unit, ga.Unit) {
		return SummaryValue{
			Value:   math.Abs(sm.Value) + math.Abs(ga.Value),
			Unit:    sm.Unit,
			Present: true,
		}
	}
	if okSM {
		return sm
	}
	if okGA {
		return ga
	}
	return SummaryValue{}
}

func deriveQuarterlyIntangibles(summary *FinancialSummary, s *store.Store, facts []summaryFact) {
	if summary == nil || len(summary.Periods) == 0 {
		return
	}
	n := len(summary.Periods)
	currency := summaryCurrency(*summary)
	if currency == "" {
		currency = "USD"
	}

	// Annual base intangible calculation for baseline
	var annualAmort, annualCap float64
	if s != nil {
		if annualSummary, err := LoadFinancialSummary(context.Background(), s, 5, summary.Ticker); err == nil && len(annualSummary.Periods) > 0 {
			if amortM := namedMetric(annualSummary, "Amortization of Intangibles"); len(amortM) > 0 && amortM[0].Present {
				annualAmort = amortM[0].Value
			}
			if capM := namedMetric(annualSummary, "Intangible Capital"); len(capM) > 0 && capM[0].Present {
				annualCap = capM[0].Value
			}
		}
	}

	quarterlyAmort := annualAmort / 4.0

	amortValues := make([]SummaryValue, n)
	adjEBITValues := make([]SummaryValue, n)
	capValues := make([]SummaryValue, n)
	investedCapValues := make([]SummaryValue, n)
	adjROICValues := make([]SummaryValue, n)

	opIncome := namedMetric(*summary, "Operating Income")
	rd := namedMetric(*summary, "Research & Development")
	sga := namedMetric(*summary, "Selling, General & Administrative")
	equity := namedMetric(*summary, "Stockholders' Equity")
	debt := namedMetric(*summary, "Debt / Borrowings")
	cash := namedMetric(*summary, "Cash & Equivalents")
	tax := namedMetric(*summary, "Income Tax Expense")
	pretax := namedMetric(*summary, "Income Before Tax")

	for i := 0; i < n; i++ {
		amortValues[i] = SummaryValue{Value: quarterlyAmort, Unit: currency, Present: annualAmort > 0}
		capValues[i] = SummaryValue{Value: annualCap, Unit: currency, Present: annualCap > 0}

		if i < len(opIncome) && opIncome[i].Present {
			currentRD := 0.0
			if i < len(rd) && rd[i].Present {
				currentRD = math.Abs(rd[i].Value)
			}
			currentSGA := 0.0
			if i < len(sga) && sga[i].Present {
				currentSGA = math.Abs(sga[i].Value)
			}
			adjEBIT := opIncome[i].Value + currentRD + (0.30 * currentSGA) - quarterlyAmort
			adjEBITValues[i] = SummaryValue{Value: adjEBIT, Unit: opIncome[i].Unit, Present: true}
		}

		if i < len(equity) && equity[i].Present {
			d := 0.0
			if i < len(debt) && debt[i].Present {
				d = debt[i].Value
			}
			c := 0.0
			if i < len(cash) && cash[i].Present {
				c = cash[i].Value
			}
			invCap := equity[i].Value + d - c + annualCap
			investedCapValues[i] = SummaryValue{Value: invCap, Unit: equity[i].Unit, Present: true}
		}

		// Annualized quarterly ROIC = (Quarterly AdjEBIT * 4) * (1 - taxRate) / Invested Capital
		if adjEBITValues[i].Present && investedCapValues[i].Present && investedCapValues[i].Value > 0 {
			taxRate := 0.21
			if i < len(tax) && i < len(pretax) && tax[i].Present && pretax[i].Present && pretax[i].Value > 0 {
				tr := math.Abs(tax[i].Value) / pretax[i].Value
				if tr >= 0 && tr <= 0.50 {
					taxRate = tr
				}
			}
			annualizedEBIT := adjEBITValues[i].Value * 4.0
			roic := (annualizedEBIT * (1.0 - taxRate)) / investedCapValues[i].Value
			if finite(roic) {
				adjROICValues[i] = SummaryValue{Value: roic, Unit: "%", Present: true}
			}
		}
	}

	setOrAppendMetric(summary, "Amortization of Intangibles", amortValues)
	setOrAppendMetric(summary, "Adjusted Operating Income", adjEBITValues)
	setOrAppendMetric(summary, "Intangible Capital", capValues)
	setOrAppendMetric(summary, "Adjusted Invested Capital", investedCapValues)
	setOrAppendMetric(summary, "Intangible-Adjusted ROIC", adjROICValues)
}

// LoadTTMSummary loads Trailing-Twelve-Month (TTM) fundamental metrics alongside
// quarterly inflection points and surprises.
func LoadTTMSummary(ctx context.Context, s *store.Store, ticker ...string) (TTMSummary, error) {
	qSummary, err := LoadQuarterlySummary(ctx, s, 8, ticker...)
	if err != nil {
		return TTMSummary{}, err
	}

	var res TTMSummary
	res.Company = qSummary.Company
	res.Ticker = qSummary.Ticker

	latestQuarter := SummaryPeriod{Label: "TTM", Quarter: "TTM", Year: "TTM"}
	if len(qSummary.Periods) > 0 {
		latestQuarter.End = qSummary.Periods[0].End
		latestQuarter.StartDate = qSummary.Periods[min(3, len(qSummary.Periods)-1)].StartDate
	}
	res.TTMPeriod = latestQuarter

	numQuarters := min(4, len(qSummary.Periods))

	instantMetrics := map[string]bool{
		"Cash & Equivalents":              true,
		"Current Assets":                  true,
		"Current Liabilities":             true,
		"Total Assets":                    true,
		"Debt / Borrowings":               true,
		"Stockholders' Equity":            true,
		"Shares Outstanding":              true,
		"Diluted Weighted Average Shares": true,
		"Intangible Capital":              true,
		"Adjusted Invested Capital":       true,
		"Intangible-Adjusted ROIC":        true,
	}

	for _, qMetric := range qSummary.Metrics {
		var ttmVal SummaryValue
		if instantMetrics[qMetric.Name] {
			if len(qMetric.Values) > 0 && qMetric.Values[0].Present {
				ttmVal = qMetric.Values[0]
			}
		} else {
			// Sum flow metrics across 4 quarters
			sum := 0.0
			count := 0
			unit := ""
			for i := 0; i < numQuarters; i++ {
				if i < len(qMetric.Values) && qMetric.Values[i].Present {
					sum += qMetric.Values[i].Value
					count++
					if unit == "" {
						unit = qMetric.Values[i].Unit
					}
				}
			}
			if count > 0 {
				if count < 4 && count >= 2 {
					sum = (sum / float64(count)) * 4.0
				}
				ttmVal = SummaryValue{Value: sum, Unit: unit, Present: true}
			}
		}
		res.Metrics = append(res.Metrics, SummaryMetric{
			Name:   qMetric.Name,
			Values: []SummaryValue{ttmVal},
		})
	}

	// Recalculate TTM Intangible-Adjusted ROIC
	adjEBITM := summaryMetricValue(res.Metrics, "Adjusted Operating Income", 0)
	investedCapM := summaryMetricValue(res.Metrics, "Adjusted Invested Capital", 0)
	taxM := summaryMetricValue(res.Metrics, "Income Tax Expense", 0)
	pretaxM := summaryMetricValue(res.Metrics, "Income Before Tax", 0)

	if adjEBITM.Present && investedCapM.Present && investedCapM.Value > 0 {
		taxRate := 0.21
		if taxM.Present && pretaxM.Present && pretaxM.Value > 0 {
			tr := math.Abs(taxM.Value) / pretaxM.Value
			if tr >= 0 && tr <= 0.50 {
				taxRate = tr
			}
		}
		ttmROIC := (adjEBITM.Value * (1.0 - taxRate)) / investedCapM.Value
		if finite(ttmROIC) {
			for i := range res.Metrics {
				if res.Metrics[i].Name == "Intangible-Adjusted ROIC" {
					res.Metrics[i].Values[0] = SummaryValue{Value: ttmROIC, Unit: "%", Present: true}
				}
			}
		}
	}

	// Compute Quarterly Inflections (YoY Acceleration, QoQ Growth)
	inflectionTargets := []string{
		"Revenue",
		"Operating Income",
		"Net Income",
		"Operating Cash Flow",
		"Free Cash Flow",
	}

	for _, target := range inflectionTargets {
		vals := namedMetric(qSummary, target)
		if len(vals) < 5 {
			continue
		}
		q0 := vals[0] // Latest quarter
		q1 := vals[1] // Prior quarter
		q4 := vals[4] // Prior year same quarter

		if !q0.Present || !q4.Present || q4.Value == 0 {
			continue
		}

		yoyGrowth := (q0.Value - q4.Value) / math.Abs(q4.Value)
		qoqGrowth := 0.0
		if q1.Present && q1.Value != 0 {
			qoqGrowth = (q0.Value - q1.Value) / math.Abs(q1.Value)
		}

		priorYoY := 0.0
		isAccel := false
		if len(vals) >= 6 && vals[1].Present && vals[5].Present && vals[5].Value != 0 {
			priorYoY = (vals[1].Value - vals[5].Value) / math.Abs(vals[5].Value)
			isAccel = yoyGrowth > priorYoY
		}

		res.Inflections = append(res.Inflections, QuarterlyInflection{
			Metric:                target,
			LatestValue:           q0.Value,
			PriorQuarterValue:     q1.Value,
			PriorYearQuarterValue: q4.Value,
			QoQGrowth:             qoqGrowth,
			YoYGrowth:             yoyGrowth,
			PriorYoYGrowth:        priorYoY,
			IsAccelerating:        isAccel,
		})
	}

	return res, nil
}

// LoadTTMFinancialSummary creates a FinancialSummary presentation where column 0 is TTM
// followed by the recent annual periods, perfect for comparative financial statements.
func LoadTTMFinancialSummary(ctx context.Context, s *store.Store, ticker ...string) (FinancialSummary, error) {
	annualSummary, err := LoadFinancialSummary(ctx, s, 4, ticker...)
	if err != nil {
		return FinancialSummary{}, err
	}

	ttm, err := LoadTTMSummary(ctx, s, ticker...)
	if err != nil {
		return annualSummary, nil
	}

	ttmPeriod := SummaryPeriod{
		Year:      "TTM",
		End:       ttm.TTMPeriod.End,
		Quarter:   "TTM",
		Label:     "TTM",
		Form:      "TTM",
		StartDate: ttm.TTMPeriod.StartDate,
	}

	combined := FinancialSummary{
		Company: annualSummary.Company,
		Ticker:  annualSummary.Ticker,
		Periods: append([]SummaryPeriod{ttmPeriod}, annualSummary.Periods...),
	}

	for _, def := range summaryMetricDefinitions {
		ttmVal := SummaryValue{}
		for _, m := range ttm.Metrics {
			if m.Name == def.name && len(m.Values) > 0 {
				ttmVal = m.Values[0]
				break
			}
		}

		annualVals := namedMetric(annualSummary, def.name)
		allVals := append([]SummaryValue{ttmVal}, annualVals...)
		combined.Metrics = append(combined.Metrics, SummaryMetric{
			Name:   def.name,
			Values: allVals,
		})
	}

	return combined, nil
}

// WriteTTMInflections writes a formatted table of quarterly inflection points to output.
func WriteTTMInflections(output io.Writer, inflections []QuarterlyInflection) error {
	if len(inflections) == 0 {
		return nil
	}
	fmt.Fprintln(output, "\nQuarterly Inflections & Earnings Acceleration (YoY):")
	tw := tabwriter.NewWriter(output, 0, 4, 3, ' ', 0)
	fmt.Fprintln(tw, "METRIC\tLATEST (Q0)\tPRIOR Q (Q1)\tYoY (Q0 vs Q4)\tPRIOR YoY\tQoQ\tINFLECTION")
	for _, inf := range inflections {
		accelStr := "steady"
		if inf.IsAccelerating {
			accelStr = "ACCELERATING ▲"
		} else if inf.YoYGrowth < inf.PriorYoYGrowth-0.05 {
			accelStr = "decelerating ▼"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%+.1f%%\t%+.1f%%\t%+.1f%%\t%s\n",
			inf.Metric,
			formatSummaryValue(inf.LatestValue, ""),
			formatSummaryValue(inf.PriorQuarterValue, ""),
			inf.YoYGrowth*100,
			inf.PriorYoYGrowth*100,
			inf.QoQGrowth*100,
			accelStr,
		)
	}
	return tw.Flush()
}

