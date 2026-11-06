package sec

import (
	"fmt"
	"io"
	"math"
	"strings"
	"text/tabwriter"
)

type DCFConfig struct {
	GrowthRate          float64
	UseHistoricalGrowth bool
	DiscountRate        float64
	TerminalRate        float64
	Years               int
}

type DCFProjection struct {
	Year         int
	FreeCashFlow float64
	PresentValue float64
}

type DCFSensitivity struct {
	DiscountRates       []float64
	TerminalGrowthRates []float64
	PerShare            [][]float64
	Valid               [][]bool
}

type DCFResult struct {
	Company              string
	Ticker               string
	BaseYear             string
	Currency             string
	GrowthRate           float64
	GrowthSource         string
	HistoricalFCFCAGR    float64
	HasHistoricalFCFCAGR bool
	DiscountRate         float64
	TerminalRate         float64
	Years                int
	BaseFCF              float64
	Projections          []DCFProjection
	PVExplicitCashFlows  float64
	TerminalValue        float64
	PVTerminalValue      float64
	EnterpriseValue      float64
	Cash                 float64
	Debt                 float64
	EquityValue          float64
	SharesOutstanding    float64
	ValuePerShare        float64
	Sensitivity          DCFSensitivity
}

// CalculateDCF calculates a two-stage enterprise-value-to-equity-value DCF
// from the latest annual facts in summary.
func CalculateDCF(summary FinancialSummary, config DCFConfig) (DCFResult, error) {
	var result DCFResult
	if config.Years < 1 || config.Years > 20 {
		return result, fmt.Errorf("forecast years must be between 1 and 20")
	}
	if !finite(config.DiscountRate) || config.DiscountRate <= 0 || config.DiscountRate >= 1 {
		return result, fmt.Errorf("discount rate must be greater than 0%% and less than 100%%")
	}
	if !finite(config.TerminalRate) || config.TerminalRate < 0 || config.TerminalRate >= config.DiscountRate {
		return result, fmt.Errorf("terminal growth must be non-negative and below the discount rate")
	}

	fcf := metricValues(summary, "Free Cash Flow")
	cashValues := metricValues(summary, "Cash & Equivalents")
	debtValues := metricValues(summary, "Debt / Borrowings")
	sharesValues := metricValues(summary, "Shares Outstanding")
	if len(summary.Periods) == 0 || len(fcf) == 0 || !fcf[0].Present || fcf[0].Value <= 0 {
		return result, fmt.Errorf("a positive latest annual free cash flow is required for DCF")
	}
	if len(cashValues) == 0 || !cashValues[0].Present {
		return result, fmt.Errorf("latest annual cash and equivalents are missing")
	}
	if len(debtValues) == 0 || !debtValues[0].Present {
		return result, fmt.Errorf("latest annual debt or borrowings are missing")
	}
	if len(sharesValues) == 0 || !sharesValues[0].Present || sharesValues[0].Value <= 0 {
		return result, fmt.Errorf("latest shares outstanding are missing or invalid")
	}
	currency := strings.TrimSpace(fcf[0].Unit)
	if currency == "" || strings.EqualFold(currency, "shares") ||
		!strings.EqualFold(currency, cashValues[0].Unit) || !strings.EqualFold(currency, debtValues[0].Unit) {
		return result, fmt.Errorf("free cash flow, cash, and debt must use the same currency unit")
	}
	if !strings.EqualFold(sharesValues[0].Unit, "shares") {
		return result, fmt.Errorf("shares outstanding must use shares as its unit")
	}

	historicalGrowth, hasHistoricalGrowth := historicalFCFCAGR(summary)
	growthRate := config.GrowthRate
	growthSource := "manual assumption"
	if config.UseHistoricalGrowth {
		if !hasHistoricalGrowth {
			return result, fmt.Errorf("growth was omitted and a three-year historical FCF CAGR is unavailable")
		}
		growthRate = historicalGrowth
		growthSource = "three-year historical FCF CAGR"
	}
	if !finite(growthRate) || growthRate <= -1 {
		return result, fmt.Errorf("forecast growth must be finite and greater than -100%%")
	}

	result = DCFResult{
		Company:              summary.Company,
		Ticker:               summary.Ticker,
		BaseYear:             summary.Periods[0].Year,
		Currency:             currency,
		GrowthRate:           growthRate,
		GrowthSource:         growthSource,
		HistoricalFCFCAGR:    historicalGrowth,
		HasHistoricalFCFCAGR: hasHistoricalGrowth,
		DiscountRate:         config.DiscountRate,
		TerminalRate:         config.TerminalRate,
		Years:                config.Years,
		BaseFCF:              fcf[0].Value,
		Cash:                 cashValues[0].Value,
		Debt:                 debtValues[0].Value,
		SharesOutstanding:    sharesValues[0].Value,
		Projections:          make([]DCFProjection, 0, config.Years),
	}

	for year := 1; year <= config.Years; year++ {
		forecast := result.BaseFCF * math.Pow(1+growthRate, float64(year))
		presentValue := forecast / math.Pow(1+config.DiscountRate, float64(year))
		if !finite(forecast) || !finite(presentValue) {
			return DCFResult{}, fmt.Errorf("DCF forecast overflowed; reduce growth or forecast years")
		}
		result.Projections = append(result.Projections, DCFProjection{
			Year:         year,
			FreeCashFlow: forecast,
			PresentValue: presentValue,
		})
		result.PVExplicitCashFlows += presentValue
	}
	lastFCF := result.Projections[len(result.Projections)-1].FreeCashFlow
	result.TerminalValue = lastFCF * (1 + config.TerminalRate) / (config.DiscountRate - config.TerminalRate)
	result.PVTerminalValue = result.TerminalValue / math.Pow(1+config.DiscountRate, float64(config.Years))
	result.EnterpriseValue = result.PVExplicitCashFlows + result.PVTerminalValue
	result.EquityValue = result.EnterpriseValue + result.Cash - result.Debt
	result.ValuePerShare = result.EquityValue / result.SharesOutstanding
	for _, value := range []float64{result.TerminalValue, result.PVTerminalValue, result.EnterpriseValue, result.EquityValue, result.ValuePerShare} {
		if !finite(value) {
			return DCFResult{}, fmt.Errorf("DCF valuation overflowed; review the assumptions")
		}
	}
	result.Sensitivity = calculateDCFSensitivity(result)
	return result, nil
}

func historicalFCFCAGR(summary FinancialSummary) (float64, bool) {
	if len(summary.Periods) < 4 || !consecutiveAnnualPeriods(summary.Periods[:4]) {
		return 0, false
	}
	fcf := metricValues(summary, "Free Cash Flow")
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
		DiscountRates:       discounts,
		TerminalGrowthRates: terminals,
		PerShare:            make([][]float64, len(discounts)),
		Valid:               make([][]bool, len(discounts)),
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
	for year := 1; year <= result.Years; year++ {
		fcf := result.BaseFCF * math.Pow(1+result.GrowthRate, float64(year))
		pvExplicit += fcf / math.Pow(1+discountRate, float64(year))
	}
	lastFCF := result.BaseFCF * math.Pow(1+result.GrowthRate, float64(result.Years))
	terminalValue := lastFCF * (1 + terminalRate) / (discountRate - terminalRate)
	pvTerminal := terminalValue / math.Pow(1+discountRate, float64(result.Years))
	value := (pvExplicit + pvTerminal + result.Cash - result.Debt) / result.SharesOutstanding
	return value, finite(value)
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func WriteDCFResult(output io.Writer, result DCFResult) error {
	if _, err := fmt.Fprintf(output, "%s (%s) — Discounted Cash Flow Valuation\n\n", result.Company, result.Ticker); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "Assumptions:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "  Base FCF (FY %s):\t%s\n", result.BaseYear, formatDCFMoney(result.BaseFCF, result.Currency)); err != nil {
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
	if _, err := fmt.Fprintf(output, "  Discount rate:\t%7.2f%%\n  Terminal growth:\t%7.2f%%\n  Forecast period:\t%d years\n\n",
		result.DiscountRate*100, result.TerminalRate*100, result.Years); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "Cash Flow Forecast:"); err != nil {
		return err
	}
	for _, projection := range result.Projections {
		if _, err := fmt.Fprintf(output, "  Year %-2d:\t%s\t(PV: %s)\n", projection.Year,
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
	_, err := fmt.Fprintln(output, "Cash flow input is operating cash flow minus CapEx; review the cash-flow definition before treating this as unlevered FCFF.")
	return err
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
