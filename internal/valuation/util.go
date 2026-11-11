package valuation

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"sec/internal/fundamentals"
)

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func metricValues(summary fundamentals.FinancialSummary, name string) []fundamentals.SummaryValue {
	return fundamentals.NamedMetric(summary, name)
}

func consecutiveAnnualPeriods(periods []fundamentals.SummaryPeriod, requested ...int) bool {
	count := 4
	if len(requested) > 0 {
		count = requested[0]
	}
	if count < 2 || len(periods) < count {
		return false
	}
	var newestYear int
	for i := 0; i < count; i++ {
		year, err := strconv.Atoi(periods[i].Year)
		if err != nil {
			return false
		}
		if i == 0 {
			newestYear = year
		}
		if year != newestYear-i {
			return false
		}
	}
	return true
}

func formatSummaryValue(val float64, unit string) string {
	u := strings.TrimSpace(unit)
	if u == "" {
		u = "USD"
	}
	abs := math.Abs(val)
	sign := ""
	if val < 0 {
		sign = "-"
	}
	if strings.EqualFold(u, "shares") {
		switch {
		case abs >= 1e9:
			return fmt.Sprintf("%s%.2fB shares", sign, abs/1e9)
		case abs >= 1e6:
			return fmt.Sprintf("%s%.2fM shares", sign, abs/1e6)
		case abs >= 1e3:
			return fmt.Sprintf("%s%.2fK shares", sign, abs/1e3)
		default:
			return fmt.Sprintf("%s%.0f shares", sign, abs)
		}
	}
	switch {
	case abs >= 1e12:
		return fmt.Sprintf("%s%.2fT %s", sign, abs/1e12, u)
	case abs >= 1e9:
		return fmt.Sprintf("%s%.2fB %s", sign, abs/1e9, u)
	case abs >= 1e6:
		return fmt.Sprintf("%s%.2fM %s", sign, abs/1e6, u)
	case abs >= 1e3:
		return fmt.Sprintf("%s%.2fK %s", sign, abs/1e3, u)
	default:
		return fmt.Sprintf("%s%.2f %s", sign, abs, u)
	}
}

func metricCAGR(values []fundamentals.SummaryValue, periods []fundamentals.SummaryPeriod) (float64, bool) {
	if len(values) < 2 || len(periods) < 2 || !values[0].Present || values[0].Value <= 0 {
		return 0, false
	}
	latestYear, latestYearErr := strconv.Atoi(periods[0].Year)
	if latestYearErr != nil {
		return 0, false
	}
	for index := len(values) - 1; index >= 1; index-- {
		if index >= len(periods) || !values[index].Present || values[index].Value <= 0 || !strings.EqualFold(values[0].Unit, values[index].Unit) {
			continue
		}
		oldestYear, err := strconv.Atoi(periods[index].Year)
		if err != nil || oldestYear >= latestYear {
			continue
		}
		years := float64(latestYear - oldestYear)
		cagr := math.Pow(values[0].Value/values[index].Value, 1/years) - 1
		if finite(cagr) && cagr > -1 {
			return cagr, true
		}
	}
	return 0, false
}

func latestUnit(values []fundamentals.SummaryValue) string {
	if len(values) > 0 && values[0].Present {
		return values[0].Unit
	}
	return ""
}
