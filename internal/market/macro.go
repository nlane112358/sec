package market

import "strings"

// MacroProfile holds regional benchmark rates for WACC and intrinsic valuation.
type MacroProfile struct {
	Code         string
	Name         string
	RiskFreeRate float64 // percentage, e.g. 4.84 for 4.84%
	TaxRate      float64 // percentage, e.g. 21.0 for 21%
	ERP          float64 // percentage, e.g. 5.5 for 5.5%
}

var MacroProfiles = map[string]MacroProfile{
	"US": {
		Code:         "US",
		Name:         "United States (US 10Y Treasury, 21% Federal Corp Tax)",
		RiskFreeRate: 4.84,
		TaxRate:      21.0,
		ERP:          5.5,
	},
	"EU": {
		Code:         "EU",
		Name:         "Eurozone (10Y German Bund ~2.40%, 25% Avg Corporate Tax)",
		RiskFreeRate: 2.40,
		TaxRate:      25.0,
		ERP:          5.5,
	},
	"UK": {
		Code:         "UK",
		Name:         "United Kingdom (10Y Gilt ~4.30%, 25% Statutory Corp Tax)",
		RiskFreeRate: 4.30,
		TaxRate:      25.0,
		ERP:          5.5,
	},
	"CH": {
		Code:         "CH",
		Name:         "Switzerland (10Y Swiss Bond ~0.60%, 15% Statutory Corp Tax)",
		RiskFreeRate: 0.60,
		TaxRate:      14.9,
		ERP:          5.5,
	},
}

// ResolveMacroProfile resolves a profile code (e.g. "US", "EU", "UK", "CH") or
// infers it from a currency code (e.g. "EUR" -> EU, "GBP" -> UK, "CHF" -> CH).
func ResolveMacroProfile(nameOrCurrency string) MacroProfile {
	key := strings.ToUpper(strings.TrimSpace(nameOrCurrency))
	switch key {
	case "EU", "EUR", "EURO", "EUROPE":
		return MacroProfiles["EU"]
	case "UK", "GBP", "BRITAIN", "ENGLAND":
		return MacroProfiles["UK"]
	case "CH", "CHF", "SWISS", "SWITZERLAND":
		return MacroProfiles["CH"]
	default:
		if p, ok := MacroProfiles[key]; ok {
			return p
		}
		return MacroProfiles["US"]
	}
}
