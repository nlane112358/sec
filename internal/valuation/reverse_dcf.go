package valuation

import (
	"fmt"
	"io"
	"math"
	"strings"
	"text/tabwriter"

	"sec/internal/fundamentals"
)

const DCFModeOwnerEarnings DCFMode = "owner"

type ReverseDCFResult struct {
	Ticker                string
	Company               string
	Mode                  DCFMode
	Unit                  string
	MarketCap             float64
	Debt                  float64
	Cash                  float64
	EnterpriseValue       float64
	BaseCashFlow          float64
	BaseRevenue           float64
	BaseOwnerMargin       float64
	OwnerEarningsAdjusted bool
	WACC                  float64
	DiscountRate          float64
	CostOfEquity          float64
	AfterTaxCostOfDebt    float64
	EffectiveTaxRate      float64
	TerminalRate          float64
	ForecastYears         int
	ImpliedGrowth         float64
	HistoricalCAGR        float64
	HasHistoricalCAGR     bool
	AlphaSpread           float64 // HistoricalCAGR - ImpliedGrowth
	IntrinsicAtHistCAGR   float64
	MarginOfSafety        float64
}

// SolveReverseDCF inverts the selected DCF model to find the annual growth rate
// that makes its enterprise value equal to market capitalization plus debt less cash.
func SolveReverseDCF(summary fundamentals.FinancialSummary, config DCFConfig) (ReverseDCFResult, error) {
	var result ReverseDCFResult
	if len(summary.Periods) == 0 {
		return result, fmt.Errorf("no annual periods available for %s", summary.Ticker)
	}
	if config.WACC == nil || !finite(config.WACC.MarketCap) || config.WACC.MarketCap <= 0 {
		return result, fmt.Errorf("reverse DCF requires a positive --market-cap input")
	}
	if config.Mode == "" {
		config.Mode = DCFModeOwnerEarnings
	}
	if config.Mode != DCFModeOwnerEarnings && config.Mode != DCFModeFCF && config.Mode != DCFModeRevenue {
		return result, fmt.Errorf("reverse DCF mode must be owner, fcf, or revenue")
	}
	years := config.Years
	if years == 0 {
		years = 5
	}
	if years < 1 || years > 20 {
		return result, fmt.Errorf("forecast years must be between 1 and 20")
	}
	terminal := config.TerminalRate
	if terminal == 0 {
		terminal = 0.025
	}
	if !finite(terminal) || terminal < 0 {
		return result, fmt.Errorf("terminal growth must be finite and non-negative")
	}

	waccConfig := *config.WACC
	if waccConfig.DebtOverride == nil {
		waccConfig.DebtOverride = config.DebtOverride
	}
	if !config.UseEffectiveTaxRate && config.TaxRate > 0 {
		waccConfig.TaxRate = config.TaxRate
		waccConfig.UseEffectiveTaxRate = false
	}
	waccResult, err := CalculateWACC(summary, waccConfig)
	if err != nil {
		return result, fmt.Errorf("calculate WACC for reverse DCF: %w", err)
	}
	discountRate := waccResult.Rate
	if config.DiscountRate > 0 {
		discountRate = config.DiscountRate
	}
	if terminal >= discountRate {
		return result, fmt.Errorf("terminal growth (%.2f%%) must be less than discount rate (%.2f%%)", terminal*100, discountRate*100)
	}

	unit := summaryCurrency(summary)
	cash, debt := waccResult.Cash, waccResult.GrossDebt
	enterpriseValue := waccConfig.MarketCap + debt - cash
	if enterpriseValue <= 0 {
		return result, fmt.Errorf("enterprise value (market cap + debt - cash) must be positive, got %.2f %s", enterpriseValue, unit)
	}

	var baseCashFlow, baseRevenue, baseOwnerMargin float64
	var ownerAdjusted bool
	var histCAGR float64
	var hasHistCAGR bool

	switch config.Mode {
	case DCFModeOwnerEarnings:
		ownerMetric := fundamentals.NamedMetric(summary, "SBC-Adjusted FCF Proxy")
		fcfMetric := fundamentals.NamedMetric(summary, "Free Cash Flow")
		revMetric := fundamentals.NamedMetric(summary, "Revenue")
		if len(ownerMetric) > 0 && ownerMetric[0].Present && ownerMetric[0].Value > 0 {
			baseCashFlow = ownerMetric[0].Value
			ownerAdjusted = true
		} else if len(fcfMetric) > 0 && fcfMetric[0].Present && fcfMetric[0].Value > 0 {
			baseCashFlow = fcfMetric[0].Value
			ownerAdjusted = false
		} else {
			return result, fmt.Errorf("positive base free cash flow is required for %s in reverse DCF", summary.Ticker)
		}
		if len(revMetric) > 0 && revMetric[0].Present && revMetric[0].Value > 0 {
			baseRevenue = revMetric[0].Value
			baseOwnerMargin = baseCashFlow / baseRevenue
		}
		if ownerAdjusted {
			histCAGR, hasHistCAGR = metricCAGR(ownerMetric, summary.Periods)
		}
		if !hasHistCAGR {
			histCAGR, hasHistCAGR = metricCAGR(fcfMetric, summary.Periods)
		}

	case DCFModeFCF:
		fcfValues := fundamentals.NamedMetric(summary, "Free Cash Flow")
		opcf := fundamentals.NamedMetric(summary, "Operating Cash Flow")
		capex := fundamentals.NamedMetric(summary, "Capital Expenditure")
		da := fundamentals.NamedMetric(summary, "Depreciation & Amortization")
		if len(fcfValues) == 0 || !fcfValues[0].Present || len(opcf) == 0 || !opcf[0].Present || len(capex) == 0 || !capex[0].Present {
			return result, fmt.Errorf("operating cash flow and capital expenditure are required for FCF reverse DCF")
		}
		reportedFCF := opcf[0].Value - math.Abs(capex[0].Value)
		daAvailable := len(da) > 0 && da[0].Present && strings.EqualFold(da[0].Unit, opcf[0].Unit)
		baseCashFlow = reportedFCF
		if config.NormalizeCapEx && daAvailable {
			baseCashFlow = opcf[0].Value - math.Abs(da[0].Value)
		}
		if baseCashFlow <= 0 {
			return result, fmt.Errorf("positive base FCF is required for reverse DCF on %s", summary.Ticker)
		}
		histCAGR, hasHistCAGR = historicalFCFCAGR(summary, config.NormalizeCapEx)

	case DCFModeRevenue:
		revMetric := fundamentals.NamedMetric(summary, "Revenue")
		if len(revMetric) == 0 || !revMetric[0].Present || revMetric[0].Value <= 0 {
			return result, fmt.Errorf("positive revenue is required for revenue-mode reverse DCF")
		}
		baseRevenue = revMetric[0].Value
		taxRate := waccResult.TaxRate
		if !config.UseEffectiveTaxRate && config.TaxRate > 0 {
			taxRate = config.TaxRate
		}
		_, _, _, _, calculatedBaseFCF, _, err := revenueDrivenForecast(summary, 0.05, taxRate, config.NormalizeCapEx, years, discountRate)
		if err != nil {
			return result, fmt.Errorf("setup revenue-driven model for reverse DCF: %w", err)
		}
		baseCashFlow = calculatedBaseFCF
		histCAGR, hasHistCAGR = historicalRevenueCAGR(summary)
	}

	evaluateEV := func(g float64) (float64, error) {
		switch config.Mode {
		case DCFModeOwnerEarnings:
			return dcfEnterpriseValue(baseCashFlow, 0.0, g, years, discountRate, terminal)
		case DCFModeFCF:
			wedge := 0.0
			capex := fundamentals.NamedMetric(summary, "Capital Expenditure")
			da := fundamentals.NamedMetric(summary, "Depreciation & Amortization")
			if len(capex) > 0 && len(da) > 0 && capex[0].Present && da[0].Present && !config.NormalizeCapEx {
				wedge = math.Max(math.Abs(capex[0].Value)-math.Abs(da[0].Value), 0)
			}
			return dcfEnterpriseValue(baseCashFlow, wedge, g, years, discountRate, terminal)
		case DCFModeRevenue:
			taxRate := waccResult.TaxRate
			if !config.UseEffectiveTaxRate && config.TaxRate > 0 {
				taxRate = config.TaxRate
			}
			_, _, _, _, _, projections, err := revenueDrivenForecast(summary, g, taxRate, config.NormalizeCapEx, years, discountRate)
			if err != nil {
				return 0, err
			}
			pvExplicit := 0.0
			for _, p := range projections {
				pvExplicit += p.FreeCashFlow / math.Pow(1+discountRate, float64(p.Year))
			}
			lastFCF := projections[len(projections)-1].FreeCashFlow
			terminalValue := lastFCF * (1 + terminal) / (discountRate - terminal)
			pvTerminal := terminalValue / math.Pow(1+discountRate, float64(years))
			return pvExplicit + pvTerminal, nil
		}
		return 0, fmt.Errorf("unknown mode %s", config.Mode)
	}

	lowGrowth, highGrowth := -0.90, 2.0
	targetEV := enterpriseValue
	lowEV, err := evaluateEV(lowGrowth)
	if err != nil {
		return result, fmt.Errorf("evaluate lower bound for reverse DCF: %w", err)
	}
	highEV, err := evaluateEV(highGrowth)
	if err != nil {
		return result, fmt.Errorf("evaluate upper bound for reverse DCF: %w", err)
	}

	if targetEV < lowEV {
		highGrowth = lowGrowth
		lowGrowth = -0.99
	} else if targetEV > highEV {
		lowGrowth = highGrowth
		highGrowth = 10.0
	}

	var impliedGrowth float64
	converged := false
	for iter := 0; iter < 100; iter++ {
		mid := (lowGrowth + highGrowth) / 2
		ev, err := evaluateEV(mid)
		if err != nil {
			highGrowth = mid
			continue
		}
		if math.Abs(ev-targetEV)/targetEV < 1e-5 || (highGrowth-lowGrowth) < 1e-5 {
			impliedGrowth = mid
			converged = true
			break
		}
		if ev < targetEV {
			lowGrowth = mid
		} else {
			highGrowth = mid
		}
	}
	if !converged {
		impliedGrowth = (lowGrowth + highGrowth) / 2
	}

	result = ReverseDCFResult{
		Ticker:                summary.Ticker,
		Company:               summary.Company,
		Mode:                  config.Mode,
		Unit:                  unit,
		MarketCap:             waccConfig.MarketCap,
		Debt:                  debt,
		Cash:                  cash,
		EnterpriseValue:       enterpriseValue,
		BaseCashFlow:          baseCashFlow,
		BaseRevenue:           baseRevenue,
		BaseOwnerMargin:       baseOwnerMargin,
		OwnerEarningsAdjusted: ownerAdjusted,
		WACC:                  waccResult.Rate,
		DiscountRate:          discountRate,
		CostOfEquity:          waccResult.CostOfEquity,
		AfterTaxCostOfDebt:    waccResult.CostOfDebt * (1 - waccResult.TaxRate),
		EffectiveTaxRate:      waccResult.TaxRate,
		TerminalRate:          terminal,
		ForecastYears:         years,
		ImpliedGrowth:         impliedGrowth,
		HistoricalCAGR:        histCAGR,
		HasHistoricalCAGR:     hasHistCAGR,
	}

	if hasHistCAGR {
		result.AlphaSpread = histCAGR - impliedGrowth
		clampedCAGR := math.Max(-0.50, math.Min(histCAGR, 0.40))
		evAtHistCAGR, err := evaluateEV(clampedCAGR)
		if err == nil {
			intrinsicEquity := evAtHistCAGR + cash - debt
			if intrinsicEquity > 0 {
				result.IntrinsicAtHistCAGR = intrinsicEquity
				result.MarginOfSafety = (intrinsicEquity - waccConfig.MarketCap) / waccConfig.MarketCap
			}
		}
	}

	return result, nil
}

func dcfEnterpriseValue(baseFCF, wedge, growth float64, years int, discountRate, terminalRate float64) (float64, error) {
	if discountRate <= terminalRate {
		return 0, fmt.Errorf("discount rate must exceed terminal growth rate")
	}
	pvExplicit := 0.0
	var lastFCF float64
	for year := 1; year <= years; year++ {
		factor := math.Pow(1+growth, float64(year))
		forecast := (baseFCF + wedge) * factor
		remainingWedge := wedge * factor * math.Max(0, 1-float64(year)/float64(years))
		forecast -= remainingWedge
		pvExplicit += forecast / math.Pow(1+discountRate, float64(year))
		lastFCF = forecast
	}
	terminalValue := lastFCF * (1 + terminalRate) / (discountRate - terminalRate)
	value := pvExplicit + terminalValue/math.Pow(1+discountRate, float64(years))
	if !finite(value) {
		return 0, fmt.Errorf("DCF valuation overflowed; reduce growth or forecast years")
	}
	return value, nil
}

func WriteReverseDCFResult(output io.Writer, result ReverseDCFResult) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintf(writer, "%s (%s) — Reverse DCF Implied Expectations (%s mode)\n", result.Company, result.Ticker, result.Mode); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Market Capitalization:\t%s\n", formatSummaryValue(result.MarketCap, result.Unit)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Enterprise Value:\t%s (Debt: %s, Cash: %s)\n", formatSummaryValue(result.EnterpriseValue, result.Unit), formatSummaryValue(result.Debt, result.Unit), formatSummaryValue(result.Cash, result.Unit)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Base Cash Flow (t=0):\t%s\n", formatSummaryValue(result.BaseCashFlow, result.Unit)); err != nil {
		return err
	}
	if result.Mode == DCFModeOwnerEarnings {
		marginLabel := "Owner Earnings Margin (FCF fallback)"
		if result.OwnerEarningsAdjusted {
			marginLabel = "SBC-Adjusted Owner Margin"
		}
		if _, err := fmt.Fprintf(writer, "%s:\t%.1f%%\n", marginLabel, result.BaseOwnerMargin*100); err != nil {
			return err
		}
	}
	if result.Mode == DCFModeRevenue && result.BaseRevenue > 0 {
		if _, err := fmt.Fprintf(writer, "Base Revenue (t=0):\t%s\n", formatSummaryValue(result.BaseRevenue, result.Unit)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "Discount Rate:\t%.2f%% (calculated WACC: %.2f%%; Ke: %.2f%%, Kd after-tax: %.2f%%, Effective tax: %.1f%%)\n", result.DiscountRate*100, result.WACC*100, result.CostOfEquity*100, result.AfterTaxCostOfDebt*100, result.EffectiveTaxRate*100); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Terminal Growth Rate:\t%.2f%% (after %d explicit years)\n", result.TerminalRate*100, result.ForecastYears); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Market-Implied %dY CAGR:\t%.2f%%\n", result.ForecastYears, result.ImpliedGrowth*100); err != nil {
		return err
	}
	if result.HasHistoricalCAGR {
		if _, err := fmt.Fprintf(writer, "Historical CAGR:\t%.2f%%\nAlpha Spread (historical − implied):\t%+.2f%%\n", result.HistoricalCAGR*100, result.AlphaSpread*100); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "Implied Valuation Upside/Downside:\t%.1f%% (equity value at capped historical CAGR: %s)\n", result.MarginOfSafety*100, formatSummaryValue(result.IntrinsicAtHistCAGR, result.Unit)); err != nil {
			return err
		}
	}
	return writer.Flush()
}
