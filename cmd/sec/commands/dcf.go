package commands

import (
	"context"
	"errors"
	"flag"
	"io"

	"sec/internal/fundamentals"
	"sec/internal/market"
	"sec/internal/store"
	"sec/internal/valuation"
)

func RunDCF(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("dcf", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec dcf TICKER [--reverse] [--mode owner|fcf|revenue] [--market-cap AMOUNT] [--growth PCT] [--discount PCT] [--debt AMOUNT] [--capex-mode raw|normalized] [--years N] [WACC options] [--data-dir DIR]")
	mode := flags.String("mode", "fcf", "DCF model: fcf or revenue-driven FCFF; owner is available with --reverse")
	reverse := flags.Bool("reverse", false, "solve for market-implied growth from --market-cap")
	growth := flags.Float64("growth", 0, "annual forecast growth in percent; defaults to historical three-year CAGR for the selected mode")
	discount := flags.Float64("discount", 0, "manual discount rate in percent; defaults to calculated WACC")
	terminal := flags.Float64("terminal", 2.5, "perpetual terminal growth in percent")
	years := flags.Int("years", 5, "explicit forecast period in years (1-20)")
	capexMode := flags.String("capex-mode", "raw", "CapEx forecast: raw with a terminal growth-CapEx fade, or normalized to D&A")
	normalizeCapEx := flags.Bool("normalize-capex", false, "normalize CapEx to depreciation and amortization")
	riskFree := flags.Float64("risk-free", 4.84, "risk-free rate in percent (default is the Nov. 5, 2026 US 10-year Treasury yield)")
	equityRiskPremium := flags.Float64("erp", 5.5, "equity risk premium in percent")
	beta := flags.Float64("beta", 1.0, "company beta; EDGAR does not provide beta")
	costOfDebt := flags.Float64("cost-debt", 5.0, "fallback pre-tax cost of debt in percent; historical interest/debt is used when available")
	taxRate := flags.Float64("tax", 21.0, "fallback tax rate in percent; historical effective tax rate is used by default")
	marketCap := flags.Float64("market-cap", 0, "market capitalization in reported currency units; otherwise book equity is used for WACC weights")
	debt := flags.Float64("debt", 0, "gross debt override in reported currency units; use 0 only when filings confirm no debt")
	profileFlag := flags.String("profile", "", "macro profile for risk-free and tax benchmarks: us, eu, uk, ch (auto-detected if omitted)")
	adrRatioFlag := flags.Float64("adr-ratio", 0, "ordinary shares per ADR (overrides built-in registry)")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "mode", "growth", "discount", "terminal", "years", "capex-mode", "normalize-capex", "risk-free", "erp", "beta", "cost-debt", "tax", "market-cap", "debt", "profile", "adr-ratio", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := CommandTicker(flags.Args())
	if err != nil {
		return err
	}
	if *capexMode != "raw" && *capexMode != "normalized" {
		return errors.New("--capex-mode must be raw or normalized")
	}
	if *riskFree < 0 || *riskFree >= 100 || *equityRiskPremium < 0 || *equityRiskPremium >= 100 ||
		*costOfDebt < 0 || *costOfDebt >= 100 || *taxRate < 0 || *taxRate >= 100 || *beta < 0 || *marketCap < 0 {
		return errors.New("WACC rates must be between 0 and 100 percent, and beta and market capitalization must be non-negative")
	}
	growthProvided, discountProvided, taxProvided, costDebtProvided, debtProvided, modeProvided, riskFreeProvided := false, false, false, false, false, false, false
	flags.Visit(func(parsed *flag.Flag) {
		switch parsed.Name {
		case "mode":
			modeProvided = true
		case "growth":
			growthProvided = true
		case "discount":
			discountProvided = true
		case "tax":
			taxProvided = true
		case "risk-free":
			riskFreeProvided = true
		case "cost-debt":
			costDebtProvided = true
		case "debt":
			debtProvided = true
		}
	})
	if *reverse {
		if !modeProvided {
			*mode = string(valuation.DCFModeOwnerEarnings)
		}
		if *mode != string(valuation.DCFModeOwnerEarnings) && *mode != string(valuation.DCFModeFCF) && *mode != string(valuation.DCFModeRevenue) {
			return errors.New("reverse DCF --mode must be owner, fcf, or revenue")
		}
		if growthProvided {
			return errors.New("--growth cannot be combined with --reverse because growth is solved from market capitalization")
		}
	} else if *mode != string(valuation.DCFModeFCF) && *mode != string(valuation.DCFModeRevenue) {
		return errors.New("--mode must be fcf or revenue; owner is available with --reverse")
	}
	if discountProvided && *discount <= 0 {
		return errors.New("--discount must be greater than 0 when provided")
	}
	if discountProvided && *discount >= 100 {
		return errors.New("--discount must be less than 100 percent")
	}
	if *reverse && *marketCap <= 0 {
		return errors.New("--reverse requires a positive --market-cap")
	}
	useNormalizedCapEx := *normalizeCapEx || *capexMode == "normalized"
	var debtOverride *float64
	if debtProvided {
		debtOverride = debt
	}
	waccConfig := &valuation.WACCConfig{
		RiskFreeRate:         *riskFree / 100,
		EquityRiskPremium:    *equityRiskPremium / 100,
		Beta:                 *beta,
		CostOfDebt:           *costOfDebt / 100,
		TaxRate:              *taxRate / 100,
		MarketCap:            *marketCap,
		DebtOverride:         debtOverride,
		UseEffectiveTaxRate:  !taxProvided,
		UseEstimatedCostDebt: !costDebtProvided,
	}
	config := valuation.DCFConfig{
		Mode:                valuation.DCFMode(*mode),
		GrowthRate:          *growth / 100,
		UseHistoricalGrowth: !growthProvided,
		DiscountRate:        *discount / 100,
		TerminalRate:        *terminal / 100,
		Years:               *years,
		NormalizeCapEx:      useNormalizedCapEx,
		DebtOverride:        debtOverride,
		TaxRate:             *taxRate / 100,
		UseEffectiveTaxRate: !taxProvided,
		WACC:                waccConfig,
	}
	databasePath := SharedDatabasePath(*dataDir)
	return WithExistingStore(parent, databasePath, ticker, func(ctx context.Context, s *store.Store) error {
		if *adrRatioFlag > 0 {
			market.RegisterADRRatio(ticker, *adrRatioFlag)
		}
		yearsToLoad := 5
		if *reverse {
			yearsToLoad = 6
		}
		summary, err := fundamentals.LoadFinancialSummary(ctx, s, yearsToLoad, ticker)
		if err != nil {
			return err
		}

		profileKey := *profileFlag
		if profileKey == "" {
			for _, m := range summary.Metrics {
				if m.Name == "Revenue" && len(m.Values) > 0 && m.Values[0].Present {
					profileKey = m.Values[0].Unit
					break
				}
			}
		}
		profile := market.ResolveMacroProfile(profileKey)
		if !riskFreeProvided {
			config.WACC.RiskFreeRate = profile.RiskFreeRate / 100
		}
		if !taxProvided {
			config.WACC.TaxRate = profile.TaxRate / 100
			config.TaxRate = profile.TaxRate / 100
		}

		if *reverse {
			result, err := valuation.SolveReverseDCF(summary, config)
			if err != nil {
				return err
			}
			return valuation.WriteReverseDCFResult(output, result)
		}
		result, err := valuation.CalculateDCF(summary, config)
		if err != nil {
			return err
		}
		return valuation.WriteDCFResult(output, result)
	})
}
