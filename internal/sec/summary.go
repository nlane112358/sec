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
	name                    string
	concepts                []summaryConcept
	freeCashFlow            bool
	ownerEarnings           bool
	sgaExpense              bool
	intangibleAmortization  bool
	adjustedOperatingIncome bool
	intangibleCapital       bool
	adjustedInvestedCapital bool
	adjustedROIC            bool
}

var summaryMetricDefinitions = []summaryMetricDefinition{
	{
		name: "Revenue",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "RevenueFromContractWithCustomerExcludingAssessedTax", duration: true},
			{taxonomy: "us-gaap", concept: "RevenueFromContractWithCustomerIncludingAssessedTax", duration: true},
			{taxonomy: "us-gaap", concept: "Revenues", duration: true},
			{taxonomy: "us-gaap", concept: "SalesRevenueNet", duration: true},
			{taxonomy: "us-gaap", concept: "SalesRevenueServicesNet", duration: true},
			{taxonomy: "us-gaap", concept: "TotalRevenuesAndOtherIncome", duration: true},
			{taxonomy: "us-gaap", concept: "RegulatedAndUnregulatedOperatingRevenue", duration: true},
			{taxonomy: "us-gaap", concept: "RealEstateRevenueNet", duration: true},
			{taxonomy: "us-gaap", concept: "PremiumsEarnedNet", duration: true},
			{taxonomy: "us-gaap", concept: "InterestAndDividendIncomeOperating", duration: true},
			{taxonomy: "ifrs-full", concept: "Revenue", duration: true},
			{taxonomy: "ifrs-full", concept: "RevenueFromContractsWithCustomers", duration: true},
		},
	},
	{
		name: "Operating Income",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "OperatingIncomeLoss", duration: true},
			{taxonomy: "ifrs-full", concept: "ProfitLossFromOperatingActivities", duration: true},
			{taxonomy: "us-gaap", concept: "IncomeLossFromContinuingOperationsBeforeIncomeTaxesMinorityInterestAndIncomeLossFromEquityMethodInvestments", duration: true},
			{taxonomy: "us-gaap", concept: "IncomeLossFromContinuingOperationsBeforeIncomeTaxesExtraordinaryItemsNoncontrollingInterest", duration: true},
			{taxonomy: "ifrs-full", concept: "ProfitLossBeforeTax", duration: true},
		},
	},
	{
		name: "Cost of Revenue",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "CostOfRevenue", duration: true},
			{taxonomy: "us-gaap", concept: "CostOfGoodsAndServicesSold", duration: true},
			{taxonomy: "us-gaap", concept: "CostOfGoodsSold", duration: true},
			{taxonomy: "us-gaap", concept: "OperatingCostsAndExpenses", duration: true},
			{taxonomy: "us-gaap", concept: "CostsAndExpenses", duration: true},
			{taxonomy: "ifrs-full", concept: "CostOfSales", duration: true},
		},
	},
	{
		name:       "Selling, General & Administrative",
		sgaExpense: true,
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "SellingGeneralAndAdministrativeExpense", duration: true},
			{taxonomy: "ifrs-full", concept: "SellingGeneralAndAdministrativeExpense", duration: true},
			{taxonomy: "ifrs-full", concept: "AdministrativeExpense", duration: true},
		},
	},
	{
		name: "Research & Development",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "ResearchAndDevelopmentExpense", duration: true},
			{taxonomy: "us-gaap", concept: "ResearchAndDevelopmentExpenseSoftwareExcludingAcquiredInProcessCost", duration: true},
			{taxonomy: "us-gaap", concept: "ResearchAndDevelopmentExpenseExcludingAcquiredInProcessCost", duration: true},
			{taxonomy: "us-gaap", concept: "ResearchDevelopmentAndEngineeringExpense", duration: true},
			{taxonomy: "us-gaap", concept: "TechnologyAndDevelopmentExpense", duration: true},
			{taxonomy: "us-gaap", concept: "SoftwareAndSoftwareDevelopmentCosts", duration: true},
			{taxonomy: "ifrs-full", concept: "ResearchAndDevelopmentExpense", duration: true},
		},
	},
	{
		name:                   "Amortization of Intangibles",
		intangibleAmortization: true,
	},
	{
		name:                    "Adjusted Operating Income",
		adjustedOperatingIncome: true,
	},
	{
		name: "Goodwill & Intangible Impairment",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "GoodwillImpairmentLoss", duration: true},
			{taxonomy: "us-gaap", concept: "GoodwillAndIntangibleAssetImpairment", duration: true},
			{taxonomy: "ifrs-full", concept: "ImpairmentLossRecognisedInProfitOrLoss", duration: true},
		},
	},
	{
		name: "Net Income",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "NetIncomeLoss", duration: true},
			{taxonomy: "us-gaap", concept: "NetIncomeLossAvailableToCommonStockholdersBasic", duration: true},
			{taxonomy: "us-gaap", concept: "ProfitLoss", duration: true},
			{taxonomy: "ifrs-full", concept: "ProfitLoss", duration: true},
			{taxonomy: "ifrs-full", concept: "ProfitLossAttributableToOwnersOfParent", duration: true},
		},
	},
	{
		name: "Operating Cash Flow",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "NetCashProvidedByUsedInOperatingActivities", duration: true},
			{taxonomy: "us-gaap", concept: "NetCashProvidedByUsedInOperatingActivitiesContinuingOperations", duration: true},
			{taxonomy: "ifrs-full", concept: "CashFlowsFromUsedInOperatingActivities", duration: true},
		},
	},
	{
		name: "Capital Expenditure",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "PaymentsToAcquirePropertyPlantAndEquipment", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsToAcquireProductiveAssets", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsToAcquireOtherPropertyPlantAndEquipment", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsToAcquireOtherProductiveAssets", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsForFlightEquipment", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsToAcquireOilAndGasProperty", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsToExploreAndDevelopOilAndGasProperties", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsForOilAndGasExplorationAndDevelopment", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsForCapitalImprovements", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsToDevelopRealEstateAssets", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsToAcquireRealEstate", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsToAcquireRealEstateAssets", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsToAcquireIntangibleAssets", duration: true},
			{taxonomy: "ifrs-full", concept: "PurchaseOfPropertyPlantAndEquipment", duration: true},
			{taxonomy: "ifrs-full", concept: "PurchaseOfPropertyPlantAndEquipmentClassifiedAsInvestingActivities", duration: true},
			{taxonomy: "us-gaap", concept: "PaymentsForProceedsFromOtherInvestingActivities", duration: true},
		},
	},
	{
		name: "Depreciation & Amortization",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "DepreciationDepletionAndAmortization", duration: true},
			{taxonomy: "us-gaap", concept: "DepreciationAndAmortization", duration: true},
			{taxonomy: "us-gaap", concept: "Depreciation", duration: true},
			{taxonomy: "us-gaap", concept: "AmortizationOfIntangibleAssets", duration: true},
			{taxonomy: "us-gaap", concept: "DepreciationAmortizationAndAccretionNet", duration: true},
			{taxonomy: "ifrs-full", concept: "DepreciationAndAmortisationExpense", duration: true},
		},
	},
	{
		name:         "Free Cash Flow",
		freeCashFlow: true,
	},
	{
		name: "Stock-Based Compensation",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "ShareBasedCompensation", duration: true},
			{taxonomy: "us-gaap", concept: "AllocatedShareBasedCompensationExpense", duration: true},
			{taxonomy: "ifrs-full", concept: "ExpenseFromSharebasedPaymentTransactionsInWhichGoodsOrServicesReceivedDidNotQualifyForRecognitionAsAssets", duration: true},
		},
	},
	{
		name:          "SBC-Adjusted FCF Proxy",
		ownerEarnings: true,
	},
	{
		name: "Cash & Equivalents",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "CashAndCashEquivalentsAtCarryingValue"},
			{taxonomy: "us-gaap", concept: "CashCashEquivalentsRestrictedCashAndRestrictedCashEquivalents"},
			{taxonomy: "us-gaap", concept: "CashCashEquivalentsAndShortTermInvestments"},
			{taxonomy: "us-gaap", concept: "CashAndShortTermInvestments"},
			{taxonomy: "us-gaap", concept: "CashAndDueFromBanks"},
			{taxonomy: "us-gaap", concept: "Cash"},
			{taxonomy: "ifrs-full", concept: "CashAndCashEquivalents"},
		},
	},
	{
		name: "Current Assets",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "AssetsCurrent"},
			{taxonomy: "ifrs-full", concept: "CurrentAssets"},
		},
	},
	{
		name: "Current Liabilities",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "LiabilitiesCurrent"},
			{taxonomy: "ifrs-full", concept: "CurrentLiabilities"},
		},
	},
	{
		name: "Total Assets",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "Assets"},
			{taxonomy: "ifrs-full", concept: "Assets"},
		},
	},
	{
		name: "Debt / Borrowings",
	},
	{
		name: "Stockholders' Equity",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "StockholdersEquity"},
			{taxonomy: "us-gaap", concept: "StockholdersEquityIncludingPortionAttributableToNoncontrollingInterest"},
			{taxonomy: "us-gaap", concept: "CommonStockholdersEquity"},
			{taxonomy: "us-gaap", concept: "PartnersCapital"},
			{taxonomy: "us-gaap", concept: "PartnersCapitalIncludingPortionAttributableToNoncontrollingInterest"},
			{taxonomy: "ifrs-full", concept: "Equity"},
			{taxonomy: "ifrs-full", concept: "EquityAttributableToOwnersOfParent"},
			{taxonomy: "ifrs-full", concept: "EquityAttributableToParent"},
		},
	},
	{
		name:              "Intangible Capital",
		intangibleCapital: true,
	},
	{
		name:                    "Adjusted Invested Capital",
		adjustedInvestedCapital: true,
	},
	{
		name:         "Intangible-Adjusted ROIC",
		adjustedROIC: true,
	},
	{
		name: "Income Tax Expense",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "IncomeTaxExpenseBenefit", duration: true},
			{taxonomy: "ifrs-full", concept: "IncomeTaxExpenseContinuingOperations", duration: true},
		},
	},
	{
		name: "Income Before Tax",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "IncomeLossFromContinuingOperationsBeforeIncomeTaxesExtraordinaryItemsNoncontrollingInterest", duration: true},
			{taxonomy: "ifrs-full", concept: "ProfitLossBeforeTax", duration: true},
		},
	},
	{
		name: "Interest Expense",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "InterestExpense", duration: true},
			{taxonomy: "us-gaap", concept: "InterestExpenseNonoperating", duration: true},
			{taxonomy: "us-gaap", concept: "InterestExpenseNonOperating", duration: true},
			{taxonomy: "us-gaap", concept: "InterestAndDebtExpense", duration: true},
			{taxonomy: "us-gaap", concept: "InterestExpenseDebt", duration: true},
			{taxonomy: "us-gaap", concept: "InterestPaidNet", duration: true},
			{taxonomy: "ifrs-full", concept: "FinanceCosts", duration: true},
		},
	},
	{
		name: "Shares Outstanding",
		concepts: []summaryConcept{
			{taxonomy: "dei", concept: "EntityCommonStockSharesOutstanding", allowSameFYYear: true},
			{taxonomy: "us-gaap", concept: "CommonStockSharesOutstanding", allowSameFYYear: true},
			{taxonomy: "ifrs-full", concept: "NumberOfSharesOutstanding", allowSameFYYear: true},
			{taxonomy: "us-gaap", concept: "WeightedAverageNumberOfDilutedSharesOutstanding", duration: true},
			{taxonomy: "us-gaap", concept: "WeightedAverageNumberOfSharesOutstandingDiluted", duration: true},
			{taxonomy: "us-gaap", concept: "WeightedAverageNumberOfSharesOutstandingBasic", duration: true},
			{taxonomy: "ifrs-full", concept: "DilutedWeightedAverageNumberOfOrdinarySharesOutstanding", duration: true},
		},
	},
	{
		name: "Diluted Weighted Average Shares",
		concepts: []summaryConcept{
			{taxonomy: "us-gaap", concept: "WeightedAverageNumberOfDilutedSharesOutstanding", duration: true},
			{taxonomy: "us-gaap", concept: "WeightedAverageNumberOfSharesOutstandingDiluted", duration: true},
			{taxonomy: "ifrs-full", concept: "DilutedWeightedAverageNumberOfOrdinarySharesOutstanding", duration: true},
		},
	},
}

// FinancialSummary reads annual facts already stored for the company and
// returns the requested number of most recent fiscal years.
func (s *Store) FinancialSummary(ctx context.Context, yearCount int, ticker ...string) (FinancialSummary, error) {
	if yearCount < 1 {
		yearCount = 5
	}
	if yearCount > 6 {
		yearCount = 6
	}
	var summary FinancialSummary
	var cik string
	var err error
	if len(ticker) > 0 && strings.TrimSpace(ticker[0]) != "" {
		cik, summary.Company, summary.Ticker, err = s.companyForTicker(ctx, ticker[0])
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT c.cik, c.company_name, t.ticker
FROM sec_tickers t JOIN sec_companies c ON c.cik = t.cik
ORDER BY t.ticker LIMIT 1`).Scan(&cik, &summary.Company, &summary.Ticker)
	}
	if err != nil {
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
	allPeriodEnds := periodEnds
	if len(periodEnds) > yearCount {
		periodEnds = periodEnds[:yearCount]
	}
	summary.Periods = make([]SummaryPeriod, 0, len(periodEnds))
	for _, end := range periodEnds {
		summary.Periods = append(summary.Periods, SummaryPeriod{Year: end[:4], End: end})
	}

	primaryCurrency := detectPrimaryCurrency(facts)

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
						Value: freeCashFlow.Value - math.Abs(sbc.Value),
						Unit:  freeCashFlow.Unit, Present: true,
					}
				}
			case definition.name == "Capital Expenditure":
				metric.Values[i], _ = selectSummaryFact(facts, definition.concepts, period.End, primaryCurrency)
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
				metric.Values[i] = selectSGA(facts, period.End, definition.concepts, primaryCurrency)
			case definition.name == "Debt / Borrowings":
				metric.Values[i] = selectDebt(facts, period.End, primaryCurrency)
			case definition.intangibleAmortization || definition.adjustedOperatingIncome ||
				definition.intangibleCapital || definition.adjustedInvestedCapital || definition.adjustedROIC:
				// Derived after standard facts are loaded.
			default:
				metric.Values[i], _ = selectSummaryFact(facts, definition.concepts, period.End, primaryCurrency)
			}
		}
		summary.Metrics = append(summary.Metrics, metric)
	}
	deriveOperatingIncome(&summary)
	sanitizeSharesOutstanding(&summary)
	deriveIntangibleMetrics(&summary, facts, allPeriodEnds)
	return summary, nil
}

// sanitizeSharesOutstanding ensures that Shares Outstanding is at least 50% of
// Diluted Weighted Average Shares for each period, preventing dual-class edge cases
// (e.g. ERIE picking 2,542 Class B voting shares instead of 46.2M Class A shares).
func sanitizeSharesOutstanding(summary *FinancialSummary) {
	if summary == nil {
		return
	}
	var sharesMetric, dilutedMetric *SummaryMetric
	for i := range summary.Metrics {
		if summary.Metrics[i].Name == "Shares Outstanding" {
			sharesMetric = &summary.Metrics[i]
		} else if summary.Metrics[i].Name == "Diluted Weighted Average Shares" {
			dilutedMetric = &summary.Metrics[i]
		}
	}
	if sharesMetric == nil || dilutedMetric == nil {
		return
	}
	for i := 0; i < len(summary.Periods); i++ {
		if i >= len(sharesMetric.Values) || i >= len(dilutedMetric.Values) {
			break
		}
		shares := sharesMetric.Values[i]
		diluted := dilutedMetric.Values[i]
		if shares.Present && diluted.Present && diluted.Value > 0 && shares.Value < 0.5*diluted.Value {
			sharesMetric.Values[i] = diluted
		} else if !shares.Present && diluted.Present && diluted.Value > 0 {
			sharesMetric.Values[i] = diluted
		}
	}
}

// deriveOperatingIncome fills absent direct operating-income facts only when
// revenue and all major expense components are available in the same currency.
func deriveOperatingIncome(summary *FinancialSummary) {
	if summary == nil {
		return
	}
	var operatingIncome, revenue, cost, sga, research, impairment []SummaryValue
	for _, metric := range summary.Metrics {
		switch metric.Name {
		case "Operating Income":
			operatingIncome = metric.Values
		case "Revenue":
			revenue = metric.Values
		case "Cost of Revenue":
			cost = metric.Values
		case "Selling, General & Administrative":
			sga = metric.Values
		case "Research & Development":
			research = metric.Values
		case "Goodwill & Intangible Impairment":
			impairment = metric.Values
		}
	}
	for i := range operatingIncome {
		if operatingIncome[i].Present || i >= len(revenue) || i >= len(cost) || i >= len(sga) || i >= len(research) {
			continue
		}
		parts := []SummaryValue{revenue[i], cost[i], sga[i], research[i]}
		valid := true
		for _, part := range parts {
			if !part.Present || !strings.EqualFold(part.Unit, revenue[i].Unit) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		value := revenue[i].Value - math.Abs(cost[i].Value) - math.Abs(sga[i].Value) - math.Abs(research[i].Value)
		if i < len(impairment) && impairment[i].Present && strings.EqualFold(impairment[i].Unit, revenue[i].Unit) {
			value -= math.Abs(impairment[i].Value)
		}
		operatingIncome[i] = SummaryValue{Value: value, Unit: revenue[i].Unit, Present: true}
	}
}

var rdConcepts = []summaryConcept{
	{taxonomy: "us-gaap", concept: "ResearchAndDevelopmentExpense", duration: true},
	{taxonomy: "us-gaap", concept: "ResearchAndDevelopmentExpenseSoftwareExcludingAcquiredInProcessCost", duration: true},
	{taxonomy: "us-gaap", concept: "ResearchAndDevelopmentExpenseExcludingAcquiredInProcessCost", duration: true},
	{taxonomy: "us-gaap", concept: "ResearchDevelopmentAndEngineeringExpense", duration: true},
	{taxonomy: "us-gaap", concept: "TechnologyAndDevelopmentExpense", duration: true},
	{taxonomy: "us-gaap", concept: "SoftwareAndSoftwareDevelopmentCosts", duration: true},
	{taxonomy: "ifrs-full", concept: "ResearchAndDevelopmentExpense", duration: true},
}

var sgaConcepts = []summaryConcept{
	{taxonomy: "us-gaap", concept: "SellingGeneralAndAdministrativeExpense", duration: true},
	{taxonomy: "ifrs-full", concept: "SellingGeneralAndAdministrativeExpense", duration: true},
	{taxonomy: "ifrs-full", concept: "AdministrativeExpense", duration: true},
}

func selectSGA(facts []summaryFact, periodEnd string, concepts []summaryConcept, preferredCurrency ...string) SummaryValue {
	if val, ok := selectSummaryFact(facts, concepts, periodEnd, preferredCurrency...); ok {
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
	sm, okSM := selectSummaryFact(facts, smConcepts, periodEnd, preferredCurrency...)
	ga, okGA := selectSummaryFact(facts, gaConcepts, periodEnd, preferredCurrency...)
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

// deriveIntangibleMetrics implements the Peters & Taylor Total Q / Damodaran
// Intangible Capitalization model:
// - R&D is capitalized with 5-year linear amortization (20%/yr).
// - 30% of SG&A is capitalized with 3-year linear amortization (33.33%/yr).
// - Intangible Capital = sum_{k=0}^4 (1 - k/5) * R&D_{t-k} + sum_{k=0}^2 (1 - k/3) * (0.30 * SG&A_{t-k}).
// - Amortization = sum_{k=1}^5 (1/5) * R&D_{t-k} + sum_{k=1}^3 (1/3) * (0.30 * SG&A_{t-k}).
// - Adjusted EBIT = Operating Income + R&D + 0.30 * SG&A - Amortization.
// - Adjusted Invested Capital = Stockholders' Equity + Debt - Cash + Intangible Capital.
// - Adjusted ROIC = Adjusted EBIT * (1 - taxRate) / Average Adjusted Invested Capital.
func deriveIntangibleMetrics(summary *FinancialSummary, facts []summaryFact, allPeriodEnds []string) {
	if summary == nil || len(summary.Periods) == 0 {
		return
	}
	allEnds := allPeriodEnds
	if len(allEnds) == 0 {
		allEnds = make([]string, len(summary.Periods))
		for j, p := range summary.Periods {
			allEnds[j] = p.End
		}
	}

	rdByEnd := make(map[string]SummaryValue)
	sgaByEnd := make(map[string]SummaryValue)
	rdByYear := make(map[string]float64)
	sgaByYear := make(map[string]float64)

	if len(facts) > 0 {
		for _, fact := range facts {
			if fact.start == "" || !secFilingForm(fact.form) || !isAnnualDuration(fact.start, fact.end) {
				continue
			}
			year := fact.end
			if len(year) >= 4 {
				year = year[:4]
			}
			for _, c := range rdConcepts {
				if matchesConceptTaxonomy(fact.taxonomy, c.taxonomy) && fact.concept == c.concept && finite(fact.value) && fact.value > 0 {
					if existing, ok := rdByYear[year]; !ok || fact.value > existing {
						rdByYear[year] = fact.value
					}
					break
				}
			}
		}
		for _, end := range allEnds {
			if v, ok := selectSummaryFact(facts, rdConcepts, end); ok {
				rdByEnd[end] = v
				if len(end) >= 4 {
					rdByYear[end[:4]] = math.Abs(v.Value)
				}
			}
			sgaVal := selectSGA(facts, end, sgaConcepts)
			if sgaVal.Present {
				sgaByEnd[end] = sgaVal
				if len(end) >= 4 {
					sgaByYear[end[:4]] = math.Abs(sgaVal.Value)
				}
			}
		}
	}

	currency := summaryCurrency(*summary)
	if currency == "" {
		currency = "USD"
	}

	rdMetric := namedMetric(*summary, "Research & Development")
	sgaMetric := namedMetric(*summary, "Selling, General & Administrative")
	opIncomeMetric := namedMetric(*summary, "Operating Income")
	hasOperatingIncome := false
	for _, v := range opIncomeMetric {
		if v.Present && finite(v.Value) {
			hasOperatingIncome = true
			break
		}
	}
	if !hasOperatingIncome {
		return
	}

	getRD := func(periodIdx, k int) (float64, bool) {
		targetIdx := periodIdx + k
		if targetIdx < len(allEnds) {
			end := allEnds[targetIdx]
			if v, ok := rdByEnd[end]; ok && v.Present {
				return math.Abs(v.Value), true
			}
			if len(end) >= 4 {
				if val, ok := rdByYear[end[:4]]; ok && val > 0 {
					return val, true
				}
			}
		}
		if targetIdx < len(rdMetric) && rdMetric[targetIdx].Present {
			return math.Abs(rdMetric[targetIdx].Value), true
		}
		return 0, false
	}

	getSGA := func(periodIdx, k int) (float64, bool) {
		targetIdx := periodIdx + k
		if targetIdx < len(allEnds) {
			end := allEnds[targetIdx]
			if v, ok := sgaByEnd[end]; ok && v.Present {
				return math.Abs(v.Value), true
			}
			if len(end) >= 4 {
				if val, ok := sgaByYear[end[:4]]; ok && val > 0 {
					return val, true
				}
			}
		}
		if targetIdx < len(sgaMetric) && sgaMetric[targetIdx].Present {
			return math.Abs(sgaMetric[targetIdx].Value), true
		}
		return 0, false
	}

	resolveRD := func(periodIdx, k int) (float64, bool) {
		if v, ok := getRD(periodIdx, k); ok {
			return v, true
		}
		// Interpolate if neighboring years are available to bridge single-year tag gaps
		if k > 0 && k < 4 {
			prev, okPrev := getRD(periodIdx, k-1)
			next, okNext := getRD(periodIdx, k+1)
			if okPrev && okNext {
				return (prev + next) / 2.0, true
			}
		}
		return 0, false
	}

	resolveSGA := func(periodIdx, k int) (float64, bool) {
		if v, ok := getSGA(periodIdx, k); ok {
			return v, true
		}
		if k > 0 && k < 2 {
			prev, okPrev := getSGA(periodIdx, k-1)
			next, okNext := getSGA(periodIdx, k+1)
			if okPrev && okNext {
				return (prev + next) / 2.0, true
			}
		}
		return 0, false
	}

	n := len(summary.Periods)
	amortValues := make([]SummaryValue, n)
	adjEBITValues := make([]SummaryValue, n)
	intangibleCapValues := make([]SummaryValue, n)
	adjInvestedCapValues := make([]SummaryValue, n)
	adjROICValues := make([]SummaryValue, n)

	equityMetric := namedMetric(*summary, "Stockholders' Equity")
	debtMetric := namedMetric(*summary, "Debt / Borrowings")
	cashMetric := namedMetric(*summary, "Cash & Equivalents")
	taxMetric := namedMetric(*summary, "Income Tax Expense")
	pretaxMetric := namedMetric(*summary, "Income Before Tax")

	for i := 0; i < n; i++ {
		// 1. R&D Capital (5-year linear life): sum_{k=0}^4 (1 - k/5) * RD_{t-k}
		rdCap := 0.0
		for k := 0; k < 5; k++ {
			if v, ok := resolveRD(i, k); ok {
				rdCap += (1.0 - float64(k)/5.0) * v
			}
		}

		// 2. SG&A Capital (3-year linear life, 30% capitalized): sum_{k=0}^2 (1 - k/3) * (0.30 * SGA_{t-k})
		sgaCap := 0.0
		for k := 0; k < 3; k++ {
			if v, ok := resolveSGA(i, k); ok {
				sgaCap += (1.0 - float64(k)/3.0) * (0.30 * v)
			}
		}
		totalIntangibleCap := rdCap + sgaCap
		intangibleCapValues[i] = SummaryValue{
			Value:   totalIntangibleCap,
			Unit:    currency,
			Present: true,
		}

		// 3. Past Amortization:
		// R&D: sum_{k=1}^5 (1/5) * RD_{t-k}
		rdAmort := 0.0
		for k := 1; k <= 5; k++ {
			if v, ok := resolveRD(i, k); ok {
				rdAmort += 0.20 * v
			}
		}
		// SG&A: sum_{k=1}^3 (1/3) * (0.30 * SGA_{t-k}) = sum_{k=1}^3 0.10 * SGA_{t-k}
		sgaAmort := 0.0
		for k := 1; k <= 3; k++ {
			if v, ok := resolveSGA(i, k); ok {
				sgaAmort += 0.10 * v
			}
		}
		totalAmort := rdAmort + sgaAmort
		amortValues[i] = SummaryValue{
			Value:   totalAmort,
			Unit:    currency,
			Present: true,
		}

		// 4. Adjusted EBIT:
		// Adjusted EBIT_t = Operating Income_t + RD_t + 0.30*SGA_t - totalAmort
		if i < len(opIncomeMetric) && opIncomeMetric[i].Present {
			currentRD, _ := resolveRD(i, 0)
			currentSGA, _ := resolveSGA(i, 0)
			adjEBIT := opIncomeMetric[i].Value + currentRD + (0.30 * currentSGA) - totalAmort
			adjEBITValues[i] = SummaryValue{
				Value:   adjEBIT,
				Unit:    opIncomeMetric[i].Unit,
				Present: true,
			}
		}

		// 5. Adjusted Invested Capital:
		// Invested Capital = Equity + Debt - Cash + Intangible Capital
		if i < len(equityMetric) && equityMetric[i].Present {
			debt := 0.0
			if i < len(debtMetric) && debtMetric[i].Present {
				debt = debtMetric[i].Value
			}
			cash := 0.0
			if i < len(cashMetric) && cashMetric[i].Present {
				cash = cashMetric[i].Value
			}
			adjInvestedCap := equityMetric[i].Value + debt - cash + totalIntangibleCap
			adjInvestedCapValues[i] = SummaryValue{
				Value:   adjInvestedCap,
				Unit:    equityMetric[i].Unit,
				Present: true,
			}
		}
	}

	for i := 0; i < n; i++ {
		if !adjEBITValues[i].Present || !adjInvestedCapValues[i].Present {
			continue
		}
		avgCap := adjInvestedCapValues[i].Value
		if i+1 < n && adjInvestedCapValues[i+1].Present {
			avgCap = (adjInvestedCapValues[i].Value + adjInvestedCapValues[i+1].Value) / 2.0
		}
		if avgCap <= 0 {
			continue
		}
		taxRate := 0.21
		if i < len(taxMetric) && i < len(pretaxMetric) && taxMetric[i].Present && pretaxMetric[i].Present &&
			pretaxMetric[i].Value > 0 {
			observed := math.Abs(taxMetric[i].Value) / pretaxMetric[i].Value
			if observed >= 0 && observed <= 0.50 {
				taxRate = observed
			}
		}
		adjROIC := (adjEBITValues[i].Value * (1.0 - taxRate)) / avgCap
		if finite(adjROIC) {
			adjROICValues[i] = SummaryValue{
				Value:   adjROIC,
				Unit:    "%",
				Present: true,
			}
		}
	}

	setOrAppendMetric(summary, "Amortization of Intangibles", amortValues)
	setOrAppendMetric(summary, "Adjusted Operating Income", adjEBITValues)
	setOrAppendMetric(summary, "Intangible Capital", intangibleCapValues)
	setOrAppendMetric(summary, "Adjusted Invested Capital", adjInvestedCapValues)
	setOrAppendMetric(summary, "Intangible-Adjusted ROIC", adjROICValues)
}

func setOrAppendMetric(summary *FinancialSummary, name string, values []SummaryValue) {
	for i := range summary.Metrics {
		if summary.Metrics[i].Name == name {
			summary.Metrics[i].Values = values
			return
		}
	}
	summary.Metrics = append(summary.Metrics, SummaryMetric{Name: name, Values: values})
}

// IntangiblesResult holds intangible capitalization and profitability metrics.
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

// CalculateIntangibles returns the Peters & Taylor / Damodaran intangible metrics
// for the given financial summary.
func CalculateIntangibles(summary FinancialSummary) IntangiblesResult {
	deriveIntangibleMetrics(&summary, nil, nil)
	currency := summaryCurrency(summary)
	if currency == "" {
		currency = "USD"
	}
	res := IntangiblesResult{
		Periods:             summary.Periods,
		Currency:            currency,
		IntangibleCapital:   namedMetric(summary, "Intangible Capital"),
		IntangibleAmort:     namedMetric(summary, "Amortization of Intangibles"),
		AdjustedEBIT:        namedMetric(summary, "Adjusted Operating Income"),
		AdjustedInvestedCap: namedMetric(summary, "Adjusted Invested Capital"),
		AdjustedROIC:        namedMetric(summary, "Intangible-Adjusted ROIC"),
	}
	ownerMetric := namedMetric(summary, "SBC-Adjusted FCF Proxy")
	fcfMetric := namedMetric(summary, "Free Cash Flow")
	rdMetric := namedMetric(summary, "Research & Development")
	sgaMetric := namedMetric(summary, "Selling, General & Administrative")
	n := len(summary.Periods)
	res.AdjustedOwnerEarnings = make([]SummaryValue, n)
	for i := 0; i < n; i++ {
		owner := SummaryValue{}
		if i < len(ownerMetric) && ownerMetric[i].Present {
			owner = ownerMetric[i]
		} else if i < len(fcfMetric) && fcfMetric[i].Present {
			owner = fcfMetric[i]
		}
		if !owner.Present {
			continue
		}
		rdVal := 0.0
		if i < len(rdMetric) && rdMetric[i].Present {
			rdVal = math.Abs(rdMetric[i].Value)
		}
		sgaVal := 0.0
		if i < len(sgaMetric) && sgaMetric[i].Present {
			sgaVal = math.Abs(sgaMetric[i].Value)
		}
		amortVal := 0.0
		if i < len(res.IntangibleAmort) && res.IntangibleAmort[i].Present {
			amortVal = res.IntangibleAmort[i].Value
		}
		growthInvestment := math.Max(0, (rdVal+0.30*sgaVal)-amortVal)
		res.AdjustedOwnerEarnings[i] = SummaryValue{
			Value:   owner.Value + growthInvestment,
			Unit:    owner.Unit,
			Present: true,
		}
	}
	return res
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

func matchesConceptTaxonomy(factTaxonomy, conceptTaxonomy string) bool {
	if conceptTaxonomy == "" || factTaxonomy == conceptTaxonomy {
		return true
	}
	// Filers often tag standard US-GAAP or IFRS concepts under their custom entity namespace (e.g. pypl)
	if (conceptTaxonomy == "us-gaap" || conceptTaxonomy == "ifrs-full") && !strings.Contains(factTaxonomy, "-") {
		return true
	}
	return false
}

func selectSummaryFact(facts []summaryFact, concepts []summaryConcept, periodEnd string, preferredCurrency ...string) (SummaryValue, bool) {
	for _, concept := range concepts {
		var matches []summaryFact
		for _, fact := range facts {
			if !matchesConceptTaxonomy(fact.taxonomy, concept.taxonomy) || fact.concept != concept.concept || !isAnnualForm(fact.form) {
				continue
			}
			if concept.duration {
				if fact.start == "" || (fact.end != periodEnd && periodDistance(fact.end, periodEnd) > 14) || !isAnnualDuration(fact.start, fact.end) {
					continue
				}
			} else if fact.start != "" || !matchingInstantPeriod(fact.end, periodEnd, concept.allowSameFYYear) {
				continue
			}
			matches = append(matches, fact)
		}
		if len(matches) == 0 && !concept.duration {
			// Second pass for instant facts: allow comparative facts in subsequent SEC filings (e.g. 10-Q)
			for _, fact := range facts {
				if !matchesConceptTaxonomy(fact.taxonomy, concept.taxonomy) || fact.concept != concept.concept || !secFilingForm(fact.form) {
					continue
				}
				if fact.start == "" && matchingInstantPeriod(fact.end, periodEnd, concept.allowSameFYYear) {
					matches = append(matches, fact)
				}
			}
		}
		if len(matches) == 0 {
			continue
		}
		best := matches[0]
		for _, candidate := range matches[1:] {
			if betterSummaryFact(candidate, best, periodEnd, preferredCurrency...) {
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
	if !allowSameFYYear {
		return periodDistance(factEnd, periodEnd) <= 14
	}
	// DEI EntityCommonStockSharesOutstanding is dated as of the 10-K cover page,
	// typically 15 to 90 days after fiscal year-end.
	if periodDistance(factEnd, periodEnd) <= 105 {
		return true
	}
	return len(factEnd) >= 4 && len(periodEnd) >= 4 && factEnd[:4] == periodEnd[:4]
}

func betterSummaryFact(candidate, current summaryFact, periodEnd string, preferredCurrency ...string) bool {
	candAnnual := isAnnualForm(candidate.form)
	currAnnual := isAnnualForm(current.form)
	if candAnnual != currAnnual {
		return candAnnual
	}
	candidateDistance := periodDistance(candidate.end, periodEnd)
	currentDistance := periodDistance(current.end, periodEnd)
	if candidateDistance != currentDistance {
		return candidateDistance < currentDistance
	}
	candidateUnitRank := summaryUnitRank(candidate.unit, preferredCurrency...)
	currentUnitRank := summaryUnitRank(current.unit, preferredCurrency...)
	if candidateUnitRank != currentUnitRank {
		return candidateUnitRank < currentUnitRank
	}
	if candidate.filed != current.filed {
		return candidate.filed > current.filed
	}
	if candidate.filed == current.filed && candidate.accession == current.accession {
		// Avoid picking tiny Class B/C share dimensions when multiple share classes share a filing
		if candidate.value != current.value {
			return candidate.value > current.value
		}
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

func detectPrimaryCurrency(facts []summaryFact) string {
	revenueConcepts := map[string]bool{
		"RevenueFromContractWithCustomerExcludingAssessedTax": true,
		"RevenueFromContractWithCustomerIncludingAssessedTax": true,
		"Revenues":                          true,
		"Revenue":                           true,
		"SalesRevenueNet":                   true,
		"SalesRevenueServicesNet":           true,
		"TotalRevenuesAndOtherIncome":       true,
		"ProfitLoss":                        true,
		"ProfitLossFromOperatingActivities": true,
		"OperatingIncomeLoss":               true,
	}
	counts := make(map[string]int)
	for _, f := range facts {
		if !isAnnualForm(f.form) || f.unit == "" || strings.EqualFold(f.unit, "shares") {
			continue
		}
		if revenueConcepts[f.concept] {
			counts[strings.ToUpper(f.unit)]++
		}
	}
	bestUnit, bestCount := "", 0
	for unit, count := range counts {
		if count > bestCount {
			bestUnit = unit
			bestCount = count
		}
	}
	if bestUnit != "" {
		return bestUnit
	}
	for _, f := range facts {
		if isAnnualForm(f.form) && f.unit != "" && !strings.EqualFold(f.unit, "shares") {
			counts[strings.ToUpper(f.unit)]++
		}
	}
	for unit, count := range counts {
		if count > bestCount {
			bestUnit = unit
			bestCount = count
		}
	}
	if bestUnit != "" {
		return bestUnit
	}
	return "USD"
}

func summaryUnitRank(unit string, preferredCurrency ...string) int {
	preferred := "USD"
	if len(preferredCurrency) > 0 && preferredCurrency[0] != "" {
		preferred = preferredCurrency[0]
	}
	if strings.EqualFold(unit, preferred) || strings.EqualFold(unit, "shares") {
		return 0
	}
	return 1
}

var balanceSheetConcepts = []string{
	"StockholdersEquity",
	"StockholdersEquityIncludingPortionAttributableToNoncontrollingInterest",
	"CommonStockholdersEquity",
	"PartnersCapital",
	"PartnersCapitalIncludingPortionAttributableToNoncontrollingInterest",
	"Equity",
	"AssetsCurrent",
	"LiabilitiesCurrent",
	"Assets",
}

func findBalanceSheetFact(facts []summaryFact, periodEnd string) (summaryFact, bool) {
	for _, f := range facts {
		if f.start != "" || periodDistance(f.end, periodEnd) > 14 {
			continue
		}
		for _, concept := range balanceSheetConcepts {
			if f.concept == concept {
				return f, true
			}
		}
	}
	return summaryFact{}, false
}

func selectDebt(facts []summaryFact, periodEnd string, preferredCurrency ...string) SummaryValue {
	if value, ok := selectDebtFact(facts, []summaryConcept{
		{taxonomy: "ifrs-full", concept: "Borrowings"},
	}, periodEnd, preferredCurrency...); ok {
		return value
	}
	if value, ok := selectDebtFact(facts, []summaryConcept{
		{taxonomy: "us-gaap", concept: "LongTermDebtNoncurrent"},
		{taxonomy: "us-gaap", concept: "LongTermDebt"},
		{taxonomy: "us-gaap", concept: "DebtAndCapitalLeaseObligations"},
		{taxonomy: "us-gaap", concept: "SeniorNotes"},
		{taxonomy: "us-gaap", concept: "SeniorLongTermNotes"},
	}, periodEnd, preferredCurrency...); ok {
		return addDebtComponents(value, facts, periodEnd, []summaryConcept{
			{taxonomy: "us-gaap", concept: "LongTermDebtCurrent"},
			{taxonomy: "us-gaap", concept: "ShortTermBorrowings"},
			{taxonomy: "us-gaap", concept: "CommercialPaper"},
			{taxonomy: "us-gaap", concept: "LinesOfCreditCurrent"},
		}, preferredCurrency...)
	}
	if value, ok := selectDebtFact(facts, []summaryConcept{
		{taxonomy: "ifrs-full", concept: "NoncurrentBorrowings"},
	}, periodEnd, preferredCurrency...); ok {
		current, currentOK := selectDebtFact(facts, []summaryConcept{
			{taxonomy: "ifrs-full", concept: "CurrentBorrowingsAndCurrentPortionOfNoncurrentBorrowings"},
			{taxonomy: "ifrs-full", concept: "CurrentPortionOfLongtermBorrowings"},
			{taxonomy: "ifrs-full", concept: "CurrentBorrowings"},
		}, periodEnd, preferredCurrency...)
		if currentOK && current.Unit == value.Unit {
			value.Value += current.Value
		}
		return value
	}
	if current, ok := selectDebtFact(facts, []summaryConcept{
		{taxonomy: "ifrs-full", concept: "CurrentBorrowingsAndCurrentPortionOfNoncurrentBorrowings"},
		{taxonomy: "ifrs-full", concept: "CurrentPortionOfLongtermBorrowings"},
		{taxonomy: "ifrs-full", concept: "CurrentBorrowings"},
	}, periodEnd, preferredCurrency...); ok {
		return current
	}
	longTermCurrent, hasLongTermCurrent := selectDebtFact(facts, []summaryConcept{
		{taxonomy: "us-gaap", concept: "LongTermDebtCurrent"},
	}, periodEnd, preferredCurrency...)
	shortTerm, hasShortTerm := selectDebtFact(facts, []summaryConcept{
		{taxonomy: "us-gaap", concept: "ShortTermBorrowings"},
		{taxonomy: "us-gaap", concept: "CommercialPaper"},
		{taxonomy: "us-gaap", concept: "LinesOfCreditCurrent"},
	}, periodEnd, preferredCurrency...)
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

	// Unlevered check: If company filed a balance sheet around this period but has zero debt tags:
	if bsFact, ok := findBalanceSheetFact(facts, periodEnd); ok {
		return SummaryValue{Value: 0.0, Unit: bsFact.unit, Present: true}
	}

	return SummaryValue{}
}

// selectDebtFact first prefers a fact from an annual filing. If the issuer only
// tagged the year-end debt balance in a later quarterly comparison, accept that
// instant fact when its period end matches the annual period (within 14 days).
func selectDebtFact(facts []summaryFact, concepts []summaryConcept, periodEnd string, preferredCurrency ...string) (SummaryValue, bool) {
	if value, ok := selectSummaryFact(facts, concepts, periodEnd, preferredCurrency...); ok {
		return value, true
	}
	for _, concept := range concepts {
		var best summaryFact
		found := false
		for _, fact := range facts {
			if fact.taxonomy != concept.taxonomy || fact.concept != concept.concept || fact.start != "" ||
				!secFilingForm(fact.form) || periodDistance(fact.end, periodEnd) > 14 {
				continue
			}
			if !found || betterSummaryFact(fact, best, periodEnd, preferredCurrency...) {
				best, found = fact, true
			}
		}
		if found {
			return SummaryValue{Value: best.value, Unit: best.unit, Present: true}, true
		}
	}
	return SummaryValue{}, false
}

func addDebtComponents(total SummaryValue, facts []summaryFact, periodEnd string, components []summaryConcept, preferredCurrency ...string) SummaryValue {
	for _, component := range components {
		value, ok := selectDebtFact(facts, []summaryConcept{component}, periodEnd, preferredCurrency...)
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
	_, err := fmt.Fprintln(output, "Free cash flow = operating cash flow − capital expenditure. SBC-adjusted FCF proxy = free cash flow − stock-based compensation; it is not a GAAP metric. Shares use period-end outstanding when available; otherwise diluted weighted-average shares. Intangibles capitalized using Peters & Taylor / Damodaran model: R&D amortized linearly over 5 years, 30% of SG&A over 3 years; Adjusted EBIT = Operating Income + R&D + 30% SG&A − Amortization.")
	return err
}

func formatSummaryValue(value float64, unit string) string {
	if unit == "%" {
		return fmt.Sprintf("%.1f%%", value*100)
	}
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
