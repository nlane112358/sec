package sec

import (
	"fmt"
	"math"
	"strings"
)

type WACCConfig struct {
	RiskFreeRate         float64
	EquityRiskPremium    float64
	Beta                 float64
	CostOfDebt           float64
	TaxRate              float64
	MarketCap            float64
	DebtOverride         *float64
	UseEffectiveTaxRate  bool
	UseEstimatedCostDebt bool
}

type WACCResult struct {
	Rate              float64
	RiskFreeRate      float64
	EquityRiskPremium float64
	Beta              float64
	CostOfEquity      float64
	CostOfDebt        float64
	TaxRate           float64
	EquityValue       float64
	Cash              float64
	GrossDebt         float64
	Debt              float64
	EquityWeight      float64
	DebtWeight        float64
	EquitySource      string
	CostOfDebtSource  string
	DebtSource        string
	TaxSource         string
	Currency          string
}

func CalculateWACC(summary FinancialSummary, config WACCConfig) (WACCResult, error) {
	var result WACCResult
	for name, rate := range map[string]float64{
		"risk-free rate":      config.RiskFreeRate,
		"equity risk premium": config.EquityRiskPremium,
		"cost of debt":        config.CostOfDebt,
		"tax rate":            config.TaxRate,
	} {
		if !finite(rate) || rate < 0 || rate >= 1 {
			return result, fmt.Errorf("%s must be between 0%% and 100%%", name)
		}
	}
	if !finite(config.Beta) || config.Beta < 0 {
		return result, fmt.Errorf("beta must be a finite non-negative value")
	}
	if !finite(config.MarketCap) || config.MarketCap < 0 {
		return result, fmt.Errorf("market capitalization must be a finite non-negative value")
	}
	if len(summary.Periods) == 0 {
		return result, fmt.Errorf("annual facts are required to estimate WACC")
	}

	debtValues := metricValues(summary, "Debt / Borrowings")
	cashValues := metricValues(summary, "Cash & Equivalents")
	debt := 0.0
	debtSource := "SEC-reported debt"
	currency := ""
	if len(debtValues) > 0 && debtValues[0].Present {
		debt = debtValues[0].Value
		currency = debtValues[0].Unit
	} else if config.DebtOverride == nil {
		return result, fmt.Errorf("debt / borrowings are missing; provide --debt if the filing confirms the amount")
	}
	if config.DebtOverride != nil {
		debt = *config.DebtOverride
		debtSource = "user-provided debt override"
	}
	if !finite(debt) || debt < 0 {
		return result, fmt.Errorf("debt override or reported debt must be finite and non-negative")
	}
	cash := 0.0
	if len(cashValues) > 0 && cashValues[0].Present {
		cash = cashValues[0].Value
		if currency == "" {
			currency = cashValues[0].Unit
		} else if !strings.EqualFold(currency, cashValues[0].Unit) {
			return result, fmt.Errorf("debt and cash must use the same currency unit")
		}
	}
	if cash < 0 {
		return result, fmt.Errorf("reported cash cannot be negative")
	}
	equityValue, equitySource := config.MarketCap, "user-provided market capitalization"
	if equityValue == 0 {
		equityValues := metricValues(summary, "Stockholders' Equity")
		if len(equityValues) == 0 || !equityValues[0].Present || equityValues[0].Value <= 0 {
			return result, fmt.Errorf("WACC needs --market-cap or positive reported stockholders' equity")
		}
		equityValue = equityValues[0].Value
		equitySource = "reported book equity proxy"
		if currency == "" {
			currency = equityValues[0].Unit
		} else if !strings.EqualFold(currency, equityValues[0].Unit) {
			return result, fmt.Errorf("debt and equity must use the same currency unit")
		}
	}
	if currency == "" {
		currency = summaryCurrency(summary)
	}
	if debtValues != nil && len(debtValues) > 0 && debtValues[0].Present && currency != "" && !strings.EqualFold(currency, debtValues[0].Unit) {
		return result, fmt.Errorf("debt and equity must use the same currency unit")
	}
	// Use net debt as the available capital-structure proxy. The DCF equity
	// bridge still subtracts gross debt and adds cash separately.
	capitalDebt := math.Max(debt-cash, 0)
	if equityValue+capitalDebt <= 0 {
		return result, fmt.Errorf("equity and debt capital must be positive")
	}

	taxRate, taxSource := config.TaxRate, "baseline tax assumption"
	if config.UseEffectiveTaxRate {
		if effectiveTax, ok := effectiveTaxRate(summary); ok {
			taxRate, taxSource = effectiveTax, "historical effective tax rate"
		}
	}
	costOfDebt, costOfDebtSource := config.CostOfDebt, "baseline cost-of-debt assumption"
	if config.UseEstimatedCostDebt {
		if estimated, ok := estimatedCostOfDebt(summary); ok {
			costOfDebt, costOfDebtSource = estimated, "historical interest/finance costs / average borrowings"
		}
	}
	if !finite(taxRate) || taxRate < 0 || taxRate >= 1 {
		return result, fmt.Errorf("effective tax rate must be between 0%% and 100%%")
	}
	if !finite(costOfDebt) || costOfDebt < 0 || costOfDebt >= 1 {
		return result, fmt.Errorf("cost of debt must be between 0%% and 100%%")
	}
	costOfEquity := config.RiskFreeRate + config.Beta*config.EquityRiskPremium
	totalCapital := equityValue + capitalDebt
	equityWeight, debtWeight := equityValue/totalCapital, capitalDebt/totalCapital
	rate := equityWeight*costOfEquity + debtWeight*costOfDebt*(1-taxRate)
	if !finite(rate) || rate <= 0 || rate >= 1 {
		return result, fmt.Errorf("calculated WACC is invalid")
	}
	return WACCResult{
		Rate:              rate,
		RiskFreeRate:      config.RiskFreeRate,
		EquityRiskPremium: config.EquityRiskPremium,
		Beta:              config.Beta,
		CostOfEquity:      costOfEquity,
		CostOfDebt:        costOfDebt,
		TaxRate:           taxRate,
		EquityValue:       equityValue,
		Cash:              cash,
		GrossDebt:         debt,
		Debt:              capitalDebt,
		EquityWeight:      equityWeight,
		DebtWeight:        debtWeight,
		EquitySource:      equitySource,
		CostOfDebtSource:  costOfDebtSource,
		DebtSource:        debtSource,
		TaxSource:         taxSource,
		Currency:          currency,
	}, nil
}

func effectiveTaxRate(summary FinancialSummary) (float64, bool) {
	taxes := metricValues(summary, "Income Tax Expense")
	pretax := metricValues(summary, "Income Before Tax")
	if len(taxes) == 0 || len(pretax) == 0 {
		return 0, false
	}
	totalTax, totalPretax := 0.0, 0.0
	for i := 0; i < minInt(3, minInt(len(taxes), len(pretax))); i++ {
		if !taxes[i].Present || !pretax[i].Present || pretax[i].Value <= 0 || taxes[i].Value < 0 ||
			!strings.EqualFold(taxes[i].Unit, pretax[i].Unit) {
			continue
		}
		periodRate := taxes[i].Value / pretax[i].Value
		if !finite(periodRate) || periodRate > 0.6 {
			continue
		}
		totalTax += taxes[i].Value
		totalPretax += pretax[i].Value
	}
	if totalPretax <= 0 {
		return 0, false
	}
	rate := totalTax / totalPretax
	return rate, finite(rate) && rate >= 0 && rate < 1
}

func estimatedCostOfDebt(summary FinancialSummary) (float64, bool) {
	interest := metricValues(summary, "Interest Expense")
	debt := metricValues(summary, "Debt / Borrowings")
	if len(interest) < 2 || len(debt) < 2 {
		return 0, false
	}
	totalInterest, totalAverageDebt := 0.0, 0.0
	periods := minInt(3, minInt(len(interest), len(debt)-1))
	for i := 0; i < periods; i++ {
		if !interest[i].Present || !debt[i].Present || !debt[i+1].Present ||
			!strings.EqualFold(interest[i].Unit, debt[i].Unit) || !strings.EqualFold(debt[i].Unit, debt[i+1].Unit) {
			continue
		}
		averageDebt := (debt[i].Value + debt[i+1].Value) / 2
		if averageDebt <= 0 {
			continue
		}
		totalInterest += math.Abs(interest[i].Value)
		totalAverageDebt += averageDebt
	}
	if totalAverageDebt <= 0 {
		return 0, false
	}
	rate := totalInterest / totalAverageDebt
	return rate, finite(rate) && rate >= 0 && rate < 1
}

func summaryCurrency(summary FinancialSummary) string {
	for _, name := range []string{"Revenue", "Free Cash Flow", "Cash & Equivalents"} {
		values := metricValues(summary, name)
		if len(values) > 0 && values[0].Present {
			return values[0].Unit
		}
	}
	return ""
}
