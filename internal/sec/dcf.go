package sec

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
)

type DCFMode string

const (
	DCFModeFCF     DCFMode = "fcf"
	DCFModeRevenue DCFMode = "revenue"
)

type DCFConfig struct {
	Mode                DCFMode
	GrowthRate          float64
	UseHistoricalGrowth bool
	DiscountRate        float64 // zero uses the calculated WACC
	TerminalRate        float64
	Years               int
	NormalizeCapEx      bool
	TaxRate             float64
	UseEffectiveTaxRate bool
	WACC                *WACCConfig
}

type DCFProjection struct {
	Year                     int
	Revenue                  float64
	EBIT                     float64
	NOPAT                    float64
	DepreciationAmortization float64
	CapitalExpenditure       float64
	ChangeInNWC              float64
	CapExWedge               float64
	FreeCashFlow             float64
	PresentValue             float64
	HasOperatingDetail       bool
}

type DCFSensitivity struct {
	DiscountRates       []float64
	TerminalGrowthRates []float64
	PerShare            [][]float64
	Valid               [][]bool
}

type DCFResult struct {
	Company                  string
	Ticker                   string
	BaseYear                 string
	Currency                 string
	Mode                     DCFMode
	NormalizeCapEx           bool
	GrowthRate               float64
	GrowthSource             string
	HistoricalFCFCAGR        float64
	HasHistoricalFCFCAGR     bool
	HistoricalRevenueCAGR    float64
	HasHistoricalRevenueCAGR bool
	OperatingMargin          float64
	EffectiveTaxRate         float64
	TaxSource                string
	CapExToRevenue           float64
	DAToRevenue              float64
	NWCToRevenue             float64
	NWCSource                string
	DiscountRate             float64
	DiscountSource           string
	WACC                     *WACCResult
	TerminalRate             float64
	Years                    int
	BaseFCF                  float64
	Projections              []DCFProjection
	PVExplicitCashFlows      float64
	TerminalValue            float64
	PVTerminalValue          float64
	EnterpriseValue          float64
	Cash                     float64
	Debt                     float64
	EquityValue              float64
	SharesOutstanding        float64
	ValuePerShare            float64
	Sensitivity              DCFSensitivity
}

// CalculateDCF calculates a two-stage DCF from the latest annual facts in summary.
// The fcf mode grows reported or D&A-normalized operating cash flow less CapEx.
// The revenue mode builds FCFF from revenue, operating margin, taxes, D&A,
// capital expenditure, and a current-assets-less-current-liabilities NWC proxy.
func CalculateDCF(summary FinancialSummary, config DCFConfig) (DCFResult, error) {
	var result DCFResult
	if config.Years < 1 || config.Years > 20 {
		return result, fmt.Errorf("forecast years must be between 1 and 20")
	}
	if config.Mode == "" {
		config.Mode = DCFModeFCF
	}
	if config.Mode != DCFModeFCF && config.Mode != DCFModeRevenue {
		return result, fmt.Errorf("DCF mode must be fcf or revenue")
	}
	if !finite(config.TerminalRate) || config.TerminalRate < 0 {
		return result, fmt.Errorf("terminal growth must be finite and non-negative")
	}

	var waccResult *WACCResult
	if config.WACC != nil {
		calculated, err := CalculateWACC(summary, *config.WACC)
		if err == nil {
			waccResult = &calculated
		} else if config.DiscountRate == 0 {
			return result, fmt.Errorf("calculate WACC: %w; provide --discount or complete the missing WACC inputs", err)
		}
	}
	discountRate, discountSource := config.DiscountRate, "manual assumption"
	if discountRate == 0 {
		if waccResult == nil {
			return result, fmt.Errorf("a discount rate or calculable WACC is required")
		}
		discountRate, discountSource = waccResult.Rate, "calculated WACC"
	}
	if !finite(discountRate) || discountRate <= 0 || discountRate >= 1 {
		return result, fmt.Errorf("discount rate must be greater than 0%% and less than 100%%")
	}
	if config.TerminalRate >= discountRate {
		return result, fmt.Errorf("terminal growth must be below the discount rate")
	}

	cash, debt, shares, currency, err := dcfBalanceSheet(summary)
	if err != nil {
		return result, err
	}
	currencyMetric := "Operating Cash Flow"
	if config.Mode == DCFModeRevenue {
		currencyMetric = "Revenue"
	}
	monetaryValues := metricValues(summary, currencyMetric)
	if len(monetaryValues) == 0 || !monetaryValues[0].Present || !strings.EqualFold(currency, monetaryValues[0].Unit) {
		return result, fmt.Errorf("%s, cash, and debt must use the same currency unit", strings.ToLower(currencyMetric))
	}
	taxRate, taxSource := config.TaxRate, "baseline tax assumption"
	if taxRate == 0 {
		taxRate = 0.21
	}
	if config.UseEffectiveTaxRate {
		if effectiveTax, ok := effectiveTaxRate(summary); ok {
			taxRate, taxSource = effectiveTax, "historical effective tax rate"
		}
	}
	if waccResult != nil {
		taxRate, taxSource = waccResult.TaxRate, waccResult.TaxSource
	}
	if !finite(taxRate) || taxRate < 0 || taxRate >= 1 {
		return result, fmt.Errorf("tax rate must be between 0%% and 100%%")
	}

	var growthRate, baseFCF, historicalGrowth float64
	var hasHistoricalGrowth bool
	var growthSource string
	var projections []DCFProjection
	var operatingMargin, daRatio, capexRatio, nwcRatio float64
	nwcSource := ""
	if config.Mode == DCFModeRevenue {
		historicalGrowth, hasHistoricalGrowth = historicalRevenueCAGR(summary)
		growthRate, growthSource = selectedGrowth(config, historicalGrowth, hasHistoricalGrowth, "three-year historical revenue CAGR")
		if config.UseHistoricalGrowth && !hasHistoricalGrowth {
			return result, fmt.Errorf("growth was omitted and a three-year historical revenue CAGR is unavailable")
		}
		if !finite(growthRate) || growthRate <= -1 {
			return result, fmt.Errorf("forecast growth must be finite and greater than -100%%")
		}
		operatingMargin, daRatio, capexRatio, nwcRatio, baseFCF, projections, err = revenueDrivenForecast(summary, growthRate, taxRate, config.NormalizeCapEx, config.Years, discountRate)
		if err != nil {
			return result, err
		}
		nwcSource = "current assets less current liabilities (SEC proxy)"
	} else {
		historicalGrowth, hasHistoricalGrowth = historicalFCFCAGR(summary, config.NormalizeCapEx)
		growthRate, growthSource = selectedGrowth(config, historicalGrowth, hasHistoricalGrowth, "three-year historical FCF CAGR")
		if config.UseHistoricalGrowth && !hasHistoricalGrowth {
			return result, fmt.Errorf("growth was omitted and a three-year historical FCF CAGR is unavailable")
		}
		if !finite(growthRate) || growthRate <= -1 {
			return result, fmt.Errorf("forecast growth must be finite and greater than -100%%")
		}
		baseFCF, projections, err = fcfForecast(summary, growthRate, config.NormalizeCapEx, config.Years, discountRate)
		if err != nil {
			return result, err
		}
	}
	if !finite(growthRate) || growthRate <= -1 {
		return result, fmt.Errorf("forecast growth must be finite and greater than -100%%")
	}

	result = DCFResult{
		Company: summary.Company, Ticker: summary.Ticker, BaseYear: summary.Periods[0].Year,
		Currency: currency, Mode: config.Mode, NormalizeCapEx: config.NormalizeCapEx,
		GrowthRate: growthRate, GrowthSource: growthSource,
		TaxSource: taxSource, EffectiveTaxRate: taxRate,
		DiscountRate: discountRate, DiscountSource: discountSource, WACC: waccResult,
		TerminalRate: config.TerminalRate, Years: config.Years, BaseFCF: baseFCF,
		OperatingMargin: operatingMargin, DAToRevenue: daRatio, CapExToRevenue: capexRatio,
		NWCToRevenue: nwcRatio, NWCSource: nwcSource,
		Cash: cash, Debt: debt, SharesOutstanding: shares, Projections: projections,
	}
	if config.Mode == DCFModeFCF {
		result.HistoricalFCFCAGR, result.HasHistoricalFCFCAGR = historicalGrowth, hasHistoricalGrowth
	} else {
		result.HistoricalRevenueCAGR, result.HasHistoricalRevenueCAGR = historicalGrowth, hasHistoricalGrowth
	}
	for i := range result.Projections {
		projection := &result.Projections[i]
		projection.PresentValue = projection.FreeCashFlow / math.Pow(1+discountRate, float64(projection.Year))
		if !finite(projection.FreeCashFlow) || !finite(projection.PresentValue) {
			return DCFResult{}, fmt.Errorf("DCF forecast overflowed; reduce growth or forecast years")
		}
		result.PVExplicitCashFlows += projection.PresentValue
	}
	lastFCF := result.Projections[len(result.Projections)-1].FreeCashFlow
	result.TerminalValue = lastFCF * (1 + config.TerminalRate) / (discountRate - config.TerminalRate)
	result.PVTerminalValue = result.TerminalValue / math.Pow(1+discountRate, float64(config.Years))
	result.EnterpriseValue = result.PVExplicitCashFlows + result.PVTerminalValue
	result.EquityValue = result.EnterpriseValue + cash - debt
	result.ValuePerShare = result.EquityValue / shares
	for _, value := range []float64{result.TerminalValue, result.PVTerminalValue, result.EnterpriseValue, result.EquityValue, result.ValuePerShare} {
		if !finite(value) {
			return DCFResult{}, fmt.Errorf("DCF valuation overflowed; review the assumptions")
		}
	}
	result.Sensitivity = calculateDCFSensitivity(result)
	return result, nil
}

func dcfBalanceSheet(summary FinancialSummary) (cash, debt, shares float64, currency string, err error) {
	if len(summary.Periods) == 0 {
		err = fmt.Errorf("annual facts are required for DCF")
		return
	}
	cashValues := metricValues(summary, "Cash & Equivalents")
	debtValues := metricValues(summary, "Debt / Borrowings")
	sharesValues := metricValues(summary, "Shares Outstanding")
	if len(cashValues) == 0 || !cashValues[0].Present {
		err = fmt.Errorf("latest annual cash and equivalents are missing")
		return
	}
	if len(debtValues) == 0 || !debtValues[0].Present {
		err = fmt.Errorf("latest annual debt or borrowings are missing")
		return
	}
	if len(sharesValues) == 0 || !sharesValues[0].Present || sharesValues[0].Value <= 0 {
		err = fmt.Errorf("latest shares outstanding are missing or invalid")
		return
	}
	currency = strings.TrimSpace(cashValues[0].Unit)
	if currency == "" || !strings.EqualFold(currency, debtValues[0].Unit) {
		err = fmt.Errorf("cash and debt must use the same currency unit")
		return
	}
	if !strings.EqualFold(sharesValues[0].Unit, "shares") {
		err = fmt.Errorf("shares outstanding must use shares as its unit")
		return
	}
	cash, debt, shares = cashValues[0].Value, debtValues[0].Value, sharesValues[0].Value
	if cash < 0 || debt < 0 || !finite(cash) || !finite(debt) || !finite(shares) {
		err = fmt.Errorf("cash, debt, and shares must be finite and non-negative")
	}
	return
}

func selectedGrowth(config DCFConfig, historical float64, available bool, source string) (float64, string) {
	if config.UseHistoricalGrowth {
		return historical, source
	}
	return config.GrowthRate, "manual assumption"
}

func fcfForecast(summary FinancialSummary, growth float64, normalizeCapEx bool, years int, discountRate float64) (float64, []DCFProjection, error) {
	fcfValues := metricValues(summary, "Free Cash Flow")
	opcf := metricValues(summary, "Operating Cash Flow")
	capex := metricValues(summary, "Capital Expenditure")
	da := metricValues(summary, "Depreciation & Amortization")
	if len(fcfValues) == 0 || !fcfValues[0].Present || len(opcf) == 0 || !opcf[0].Present || len(capex) == 0 || !capex[0].Present {
		return 0, nil, fmt.Errorf("latest annual operating cash flow and capital expenditure are required for DCF")
	}
	if !strings.EqualFold(fcfValues[0].Unit, opcf[0].Unit) || !strings.EqualFold(opcf[0].Unit, capex[0].Unit) {
		return 0, nil, fmt.Errorf("operating cash flow and capital expenditure must use the same currency unit")
	}
	reportedFCF := opcf[0].Value - math.Abs(capex[0].Value)
	daAvailable := len(da) > 0 && da[0].Present && strings.EqualFold(da[0].Unit, opcf[0].Unit)
	if normalizeCapEx && !daAvailable {
		return 0, nil, fmt.Errorf("CapEx normalization requires latest annual depreciation and amortization")
	}
	baseFCF := reportedFCF
	if normalizeCapEx {
		baseFCF = opcf[0].Value - math.Abs(da[0].Value)
	}
	if baseFCF <= 0 {
		return 0, nil, fmt.Errorf("a positive base free cash flow is required for DCF")
	}
	if !finite(baseFCF) {
		return 0, nil, fmt.Errorf("base free cash flow must be finite")
	}
	wedge := 0.0
	projectionBase := reportedFCF
	if daAvailable {
		wedge = math.Max(math.Abs(capex[0].Value)-math.Abs(da[0].Value), 0)
		projectionBase = reportedFCF + wedge
	}
	if normalizeCapEx {
		projectionBase, wedge = baseFCF, 0
	}
	if !daAvailable {
		projectionBase = reportedFCF
	}
	projections := make([]DCFProjection, 0, years)
	for year := 1; year <= years; year++ {
		factor := math.Pow(1+growth, float64(year))
		forecast := projectionBase * factor
		remainingWedge := wedge * factor * math.Max(0, 1-float64(year)/float64(years))
		forecast -= remainingWedge
		projections = append(projections, DCFProjection{Year: year, CapExWedge: remainingWedge, FreeCashFlow: forecast})
	}
	return baseFCF, projections, nil
}

func revenueDrivenForecast(summary FinancialSummary, growth, taxRate float64, normalizeCapEx bool, years int, discountRate float64) (operatingMargin, daRatio, capexRatio, nwcRatio, baseFCF float64, projections []DCFProjection, err error) {
	revenue := metricValues(summary, "Revenue")
	operatingIncome := metricValues(summary, "Operating Income")
	if len(revenue) == 0 || !revenue[0].Present || revenue[0].Value <= 0 || len(operatingIncome) == 0 || !operatingIncome[0].Present {
		err = fmt.Errorf("latest annual revenue and operating income are required for revenue-driven DCF")
		return
	}
	if !strings.EqualFold(revenue[0].Unit, operatingIncome[0].Unit) {
		err = fmt.Errorf("revenue and operating income must use the same currency unit")
		return
	}
	operatingMargin, _, ok := aggregateMetricRatio(summary, "Operating Income", "Revenue", false)
	if !ok {
		err = fmt.Errorf("historical operating margin cannot be calculated from annual facts")
		return
	}
	daRatio, _, ok = aggregateMetricRatio(summary, "Depreciation & Amortization", "Revenue", true)
	if !ok {
		err = fmt.Errorf("revenue-driven DCF requires depreciation and amortization facts")
		return
	}
	capexRatio, _, ok = aggregateMetricRatio(summary, "Capital Expenditure", "Revenue", true)
	if !ok {
		err = fmt.Errorf("revenue-driven DCF requires capital expenditure facts")
		return
	}
	nwcRatio, latestNWC, ok := historicalNWCRatio(summary)
	if !ok {
		err = fmt.Errorf("revenue-driven DCF requires current assets, current liabilities, and revenue for an NWC proxy")
		return
	}
	if normalizeCapEx {
		capexRatio = daRatio
	}
	capexWedgeRatio := math.Max(capexRatio-daRatio, 0)
	baseRevenue := revenue[0].Value
	baseDA := baseRevenue * daRatio
	baseCapEx := baseRevenue * capexRatio
	baseNWCChange := 0.0
	if priorNWC, ok := netWorkingCapitalAt(summary, 1); ok {
		baseNWCChange = latestNWC - priorNWC
	}
	baseFCF = operatingIncome[0].Value*(1-taxRate) + baseDA - baseCapEx - baseNWCChange
	projections = make([]DCFProjection, 0, years)
	priorNWC := latestNWC
	for year := 1; year <= years; year++ {
		baseRevenue *= 1 + growth
		forecastEBIT := baseRevenue * operatingMargin
		nopat := forecastEBIT * (1 - taxRate)
		forecastDA := baseRevenue * daRatio
		forecastCapExRatio := capexRatio
		wedge := 0.0
		if normalizeCapEx {
			forecastCapExRatio = daRatio
		} else if capexWedgeRatio > 0 {
			fade := float64(year) / float64(years)
			forecastCapExRatio -= capexWedgeRatio * fade
			wedge = baseRevenue * capexWedgeRatio * (1 - fade)
		}
		forecastCapEx := baseRevenue * forecastCapExRatio
		forecastNWC := baseRevenue * nwcRatio
		changeNWC := forecastNWC - priorNWC
		fcf := nopat + forecastDA - forecastCapEx - changeNWC
		presentValue := fcf / math.Pow(1+discountRate, float64(year))
		projections = append(projections, DCFProjection{
			Year: year, Revenue: baseRevenue, EBIT: forecastEBIT, NOPAT: nopat,
			DepreciationAmortization: forecastDA, CapitalExpenditure: forecastCapEx,
			ChangeInNWC: changeNWC, CapExWedge: wedge, FreeCashFlow: fcf,
			PresentValue: presentValue, HasOperatingDetail: true,
		})
		priorNWC = forecastNWC
	}
	return
}

func aggregateMetricRatio(summary FinancialSummary, numeratorName, denominatorName string, absoluteNumerator bool) (float64, int, bool) {
	numerator := metricValues(summary, numeratorName)
	denominator := metricValues(summary, denominatorName)
	limit := minInt(3, latestConsecutivePeriodCount(summary.Periods))
	if len(numerator) < limit || len(denominator) < limit || limit == 0 {
		return 0, 0, false
	}
	totalNumerator, totalDenominator, included := 0.0, 0.0, 0
	for i := 0; i < limit; i++ {
		if !numerator[i].Present || !denominator[i].Present || denominator[i].Value <= 0 ||
			!strings.EqualFold(numerator[i].Unit, denominator[i].Unit) {
			continue
		}
		value := numerator[i].Value
		if absoluteNumerator {
			value = math.Abs(value)
		}
		totalNumerator += value
		totalDenominator += denominator[i].Value
		included++
	}
	if included == 0 || totalDenominator <= 0 {
		return 0, included, false
	}
	ratio := totalNumerator / totalDenominator
	return ratio, included, finite(ratio)
}

func historicalNWCRatio(summary FinancialSummary) (float64, float64, bool) {
	assets := metricValues(summary, "Current Assets")
	liabilities := metricValues(summary, "Current Liabilities")
	revenue := metricValues(summary, "Revenue")
	limit := minInt(3, latestConsecutivePeriodCount(summary.Periods))
	if limit == 0 || len(assets) < limit || len(liabilities) < limit || len(revenue) < limit ||
		!assets[0].Present || !liabilities[0].Present || !strings.EqualFold(assets[0].Unit, liabilities[0].Unit) {
		return 0, 0, false
	}
	latest := assets[0].Value - liabilities[0].Value
	totalNWC, totalRevenue := 0.0, 0.0
	for i := 0; i < limit; i++ {
		if !assets[i].Present || !liabilities[i].Present || !revenue[i].Present || revenue[i].Value <= 0 ||
			!strings.EqualFold(assets[i].Unit, liabilities[i].Unit) || !strings.EqualFold(assets[i].Unit, revenue[i].Unit) {
			continue
		}
		totalNWC += assets[i].Value - liabilities[i].Value
		totalRevenue += revenue[i].Value
	}
	if totalRevenue <= 0 {
		return 0, 0, false
	}
	ratio := totalNWC / totalRevenue
	return ratio, latest, finite(ratio) && finite(latest)
}

func netWorkingCapitalAt(summary FinancialSummary, index int) (float64, bool) {
	assets := metricValues(summary, "Current Assets")
	liabilities := metricValues(summary, "Current Liabilities")
	if index < 0 || len(assets) <= index || len(liabilities) <= index || !assets[index].Present || !liabilities[index].Present ||
		!strings.EqualFold(assets[index].Unit, liabilities[index].Unit) {
		return 0, false
	}
	return assets[index].Value - liabilities[index].Value, true
}

func historicalFCFCAGR(summary FinancialSummary, normalize ...bool) (float64, bool) {
	normalizeCapEx := len(normalize) > 0 && normalize[0]
	if len(summary.Periods) < 4 || !consecutiveAnnualPeriods(summary.Periods[:4]) {
		return 0, false
	}
	fcf := historicalFCFValues(summary, normalizeCapEx)
	if len(fcf) < 4 {
		return 0, false
	}
	unit := fcf[0].Unit
	for i := 0; i < 4; i++ {
		if !fcf[i].Present || fcf[i].Value <= 0 || !strings.EqualFold(fcf[i].Unit, unit) {
			return 0, false
		}
	}
	rate := math.Pow(fcf[0].Value/fcf[3].Value, 1.0/3.0) - 1
	return rate, finite(rate) && rate > -1
}

func historicalFCFValues(summary FinancialSummary, normalizeCapEx bool) []SummaryValue {
	if !normalizeCapEx {
		return metricValues(summary, "Free Cash Flow")
	}
	opcf := metricValues(summary, "Operating Cash Flow")
	da := metricValues(summary, "Depreciation & Amortization")
	values := make([]SummaryValue, minInt(len(opcf), len(da)))
	for i := range values {
		if opcf[i].Present && da[i].Present && strings.EqualFold(opcf[i].Unit, da[i].Unit) {
			values[i] = SummaryValue{Value: opcf[i].Value - math.Abs(da[i].Value), Unit: opcf[i].Unit, Present: true}
		}
	}
	return values
}

func historicalRevenueCAGR(summary FinancialSummary) (float64, bool) {
	if len(summary.Periods) < 4 || !consecutiveAnnualPeriods(summary.Periods[:4]) {
		return 0, false
	}
	revenue := metricValues(summary, "Revenue")
	if len(revenue) < 4 {
		return 0, false
	}
	unit := revenue[0].Unit
	for i := 0; i < 4; i++ {
		if !revenue[i].Present || revenue[i].Value <= 0 || !strings.EqualFold(revenue[i].Unit, unit) {
			return 0, false
		}
	}
	rate := math.Pow(revenue[0].Value/revenue[3].Value, 1.0/3.0) - 1
	return rate, finite(rate) && rate > -1
}

func latestConsecutivePeriodCount(periods []SummaryPeriod) int {
	if len(periods) == 0 {
		return 0
	}
	count := 1
	previous, err := strconv.Atoi(periods[0].Year)
	if err != nil {
		return 1
	}
	for i := 1; i < len(periods); i++ {
		year, err := strconv.Atoi(periods[i].Year)
		if err != nil || year != previous-1 {
			break
		}
		count++
		previous = year
	}
	return count
}

func metricValues(summary FinancialSummary, name string) []SummaryValue {
	for _, metric := range summary.Metrics {
		if metric.Name == name {
			return metric.Values
		}
	}
	return nil
}

func calculateDCFSensitivity(result DCFResult) DCFSensitivity {
	discounts := []float64{
		math.Max(0.001, result.DiscountRate-0.01),
		result.DiscountRate,
		result.DiscountRate + 0.01,
	}
	terminals := []float64{
		math.Max(0, result.TerminalRate-0.005),
		result.TerminalRate,
		result.TerminalRate + 0.005,
	}
	sensitivity := DCFSensitivity{
		DiscountRates: discounts, TerminalGrowthRates: terminals,
		PerShare: make([][]float64, len(discounts)), Valid: make([][]bool, len(discounts)),
	}
	for row, discount := range discounts {
		sensitivity.PerShare[row] = make([]float64, len(terminals))
		sensitivity.Valid[row] = make([]bool, len(terminals))
		for column, terminal := range terminals {
			if terminal >= discount {
				continue
			}
			value, ok := dcfValuePerShare(result, discount, terminal)
			if ok {
				sensitivity.PerShare[row][column] = value
				sensitivity.Valid[row][column] = true
			}
		}
	}
	return sensitivity
}

func dcfValuePerShare(result DCFResult, discountRate, terminalRate float64) (float64, bool) {
	pvExplicit := 0.0
	for _, projection := range result.Projections {
		pvExplicit += projection.FreeCashFlow / math.Pow(1+discountRate, float64(projection.Year))
	}
	if len(result.Projections) == 0 {
		return 0, false
	}
	lastFCF := result.Projections[len(result.Projections)-1].FreeCashFlow
	terminalValue := lastFCF * (1 + terminalRate) / (discountRate - terminalRate)
	pvTerminal := terminalValue / math.Pow(1+discountRate, float64(result.Years))
	value := (pvExplicit + pvTerminal + result.Cash - result.Debt) / result.SharesOutstanding
	return value, finite(value)
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func WriteDCFResult(output io.Writer, result DCFResult) error {
	if _, err := fmt.Fprintf(output, "%s (%s) — Discounted Cash Flow Valuation\n\n", result.Company, result.Ticker); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "Assumptions:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "  Model mode:\t%s\n  CapEx mode:\t%s\n  Base FCF (FY %s):\t%s\n",
		result.Mode, capexModeName(result.NormalizeCapEx), result.BaseYear, formatDCFMoney(result.BaseFCF, result.Currency)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "  Forecast growth:\t%7.2f%% (%s)\n", result.GrowthRate*100, result.GrowthSource); err != nil {
		return err
	}
	if result.HasHistoricalFCFCAGR && result.GrowthSource != "three-year historical FCF CAGR" {
		if _, err := fmt.Fprintf(output, "  Historical 3Y FCF CAGR:\t%7.2f%%\n", result.HistoricalFCFCAGR*100); err != nil {
			return err
		}
	}
	if result.HasHistoricalRevenueCAGR && result.GrowthSource != "three-year historical revenue CAGR" {
		if _, err := fmt.Fprintf(output, "  Historical 3Y revenue CAGR:\t%7.2f%%\n", result.HistoricalRevenueCAGR*100); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(output, "  Discount rate:\t%7.2f%% (%s)\n  Terminal growth:\t%7.2f%%\n  Forecast period:\t%d years\n",
		result.DiscountRate*100, result.DiscountSource, result.TerminalRate*100, result.Years); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "  Tax rate:\t%7.2f%% (%s)\n", result.EffectiveTaxRate*100, result.TaxSource); err != nil {
		return err
	}
	if result.WACC != nil {
		if err := writeWACC(output, *result.WACC, result.Currency); err != nil {
			return err
		}
	}
	if result.Mode == DCFModeRevenue {
		if _, err := fmt.Fprintf(output, "  Operating margin:\t%7.2f%%\n  D&A / revenue:\t%7.2f%%\n  CapEx / revenue:\t%7.2f%%\n  NWC / revenue:\t%7.2f%% (%s)\n\n",
			result.OperatingMargin*100, result.DAToRevenue*100, result.CapExToRevenue*100, result.NWCToRevenue*100, result.NWCSource); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(output); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "Cash Flow Forecast:"); err != nil {
		return err
	}
	for _, projection := range result.Projections {
		if projection.HasOperatingDetail {
			if _, err := fmt.Fprintf(output, "  Year %-2d:	Revenue %s	EBIT %s	NOPAT %s	D&A %s	CapEx %s	Growth CapEx wedge %s	ΔNWC %s	FCFF %s	(PV %s)\n",
				projection.Year, formatDCFMoney(projection.Revenue, result.Currency),
				formatDCFMoney(projection.EBIT, result.Currency), formatDCFMoney(projection.NOPAT, result.Currency),
				formatDCFMoney(projection.DepreciationAmortization, result.Currency),
				formatDCFMoney(projection.CapitalExpenditure, result.Currency),
				formatDCFMoney(projection.CapExWedge, result.Currency),
				formatDCFMoney(projection.ChangeInNWC, result.Currency),
				formatDCFMoney(projection.FreeCashFlow, result.Currency), formatDCFMoney(projection.PresentValue, result.Currency)); err != nil {
				return err
			}
		} else if projection.CapExWedge > 0 {
			if _, err := fmt.Fprintf(output, "  Year %-2d:\t%s\t(growth CapEx wedge remaining: %s; PV: %s)\n", projection.Year,
				formatDCFMoney(projection.FreeCashFlow, result.Currency), formatDCFMoney(projection.CapExWedge, result.Currency),
				formatDCFMoney(projection.PresentValue, result.Currency)); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(output, "  Year %-2d:\t%s\t(PV: %s)\n", projection.Year,
			formatDCFMoney(projection.FreeCashFlow, result.Currency), formatDCFMoney(projection.PresentValue, result.Currency)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(output, "  PV of explicit cash flows:\t%s\n", formatDCFMoney(result.PVExplicitCashFlows, result.Currency)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "  Terminal value:\t%s\n  PV of terminal value:\t%s\n\n",
		formatDCFMoney(result.TerminalValue, result.Currency), formatDCFMoney(result.PVTerminalValue, result.Currency)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "Valuation Bridge:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "  Enterprise value:\t%s\n  + Cash & equivalents:\t%s\n  - Total debt:\t%s\n  Equity value:\t%s\n  Shares outstanding:\t%s\n\n",
		formatDCFMoney(result.EnterpriseValue, result.Currency), formatDCFMoney(result.Cash, result.Currency),
		formatDCFMoney(result.Debt, result.Currency), formatDCFMoney(result.EquityValue, result.Currency),
		formatDCFValue(result.SharesOutstanding, "shares")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Intrinsic Value / Share:\t%s\n\n", formatDCFPerShare(result.ValuePerShare, result.Currency)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "Sensitivity Matrix (value per share):"); err != nil {
		return err
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprint(writer, "Discount \\ Terminal"); err != nil {
		return err
	}
	for _, terminal := range result.Sensitivity.TerminalGrowthRates {
		if _, err := fmt.Fprintf(writer, "\t%.2f%%", terminal*100); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	for row, discount := range result.Sensitivity.DiscountRates {
		if _, err := fmt.Fprintf(writer, "%.2f%%", discount*100); err != nil {
			return err
		}
		for column, valid := range result.Sensitivity.Valid[row] {
			cell := "N/A"
			if valid {
				cell = formatDCFPerShare(result.Sensitivity.PerShare[row][column], result.Currency)
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
	if result.Mode == DCFModeRevenue {
		_, err := fmt.Fprintln(output, "Revenue mode uses current assets less current liabilities as an NWC proxy; review it for cash and financing items.")
		return err
	}
	_, err := fmt.Fprintln(output, "FCF mode starts with operating cash flow minus CapEx; review it before treating the result as unlevered FCFF.")
	return err
}

func writeWACC(output io.Writer, wacc WACCResult, currency string) error {
	_, err := fmt.Fprintf(output, "  WACC estimate:\t%7.2f%%\n  Risk-free / beta / ERP:\t%.2f%% / %.2f / %.2f%%\n  Cost of equity:\t%7.2f%%\n  Cost of debt:\t%7.2f%% (%s)\n  Capital weights (equity / net debt):\t%.1f%% / %.1f%%\n  Equity basis:\t%s\n  Debt / cash used for weights:\t%s / %s\n",
		wacc.Rate*100, wacc.RiskFreeRate*100, wacc.Beta, wacc.EquityRiskPremium*100,
		wacc.CostOfEquity*100, wacc.CostOfDebt*100, wacc.CostOfDebtSource,
		wacc.EquityWeight*100, wacc.DebtWeight*100, wacc.EquitySource,
		formatDCFMoney(wacc.GrossDebt, currency), formatDCFMoney(wacc.Cash, currency))
	return err
}

func capexModeName(normalize bool) string {
	if normalize {
		return "normalized to D&A"
	}
	return "raw with terminal wedge fade"
}

func formatDCFMoney(value float64, unit string) string {
	return formatDCFValue(value, unit)
}

func formatDCFValue(value float64, unit string) string {
	symbol := currencySymbol(unit)
	if strings.EqualFold(unit, "shares") {
		if math.Abs(value) >= 1e9 {
			return fmt.Sprintf("%.2fB shares", value/1e9)
		}
		return fmt.Sprintf("%.2fM shares", value/1e6)
	}
	magnitude := math.Abs(value)
	switch {
	case magnitude >= 1e12:
		return fmt.Sprintf("%s%.2fT", symbol, value/1e12)
	case magnitude >= 1e9:
		return fmt.Sprintf("%s%.2fB", symbol, value/1e9)
	case magnitude >= 1e6:
		return fmt.Sprintf("%s%.2fM", symbol, value/1e6)
	default:
		return fmt.Sprintf("%s%.2f", symbol, value)
	}
}

func formatDCFPerShare(value float64, unit string) string {
	return fmt.Sprintf("%s%.2f", currencySymbol(unit), value)
}

func currencySymbol(unit string) string {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "JPY", "CNY":
		return "¥"
	case "CAD":
		return "C$"
	case "AUD":
		return "A$"
	default:
		return unit + " "
	}
}
