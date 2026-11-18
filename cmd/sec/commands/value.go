package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"sec/internal/fundamentals"
	"sec/internal/market"
	"sec/internal/store"
	"sec/internal/valuation"
)

const valueUsage = `usage: sec value TICKER [subcommand] [options]

Subcommands:
  dcf (default)         Two-stage forward Discounted Cash Flow model
  reverse-dcf           Solve market-implied growth rate from live quote or --market-cap
  wacc                  Weighted Average Cost of Capital breakdown

Options:
  --mode fcf|revenue|owner   Cash flow model (default fcf; owner default for reverse-dcf)
  --growth PCT               Annual forecast growth in percent (default: 3Y historical CAGR)
  --discount PCT             Manual discount rate in percent (default: calculated WACC)
  --terminal PCT             Perpetual terminal growth rate in percent (default: 2.5)
  --years N                  Explicit forecast period in years (1-20, default: 5)
  --capex-mode raw|normalized
  --market-cap AMOUNT        Market capitalization override in reported currency
  --price AMOUNT             Share price override in USD
  --debt AMOUNT              Gross debt override in reported currency
  --beta N                   Company beta (default: 1.0)
  --risk-free PCT            Risk-free rate in percent (auto-detected via --profile)
  --erp PCT                  Equity risk premium in percent (default: 5.5)
  --cost-debt PCT            Fallback pre-tax cost of debt in percent
  --tax PCT                  Fallback tax rate in percent
  --profile us|eu|uk|ch      Macro benchmark profile (auto-detected if omitted)
  --adr-ratio N              Ordinary shares per ADR override
  --no-live-price            Skip live price fetch (use book equity for WACC weights)
  --refresh                  Force fresh EDGAR, quote, and FX pulls
  --offline                  Use local SQLite cache only
  --data-dir DIR             Directory containing the shared SQLite database

Examples:
  sec value AAPL
  sec value ASML dcf --mode revenue
  sec value AAPL reverse-dcf
  sec value AAPL wacc --beta 1.15
`

func isValueSubcommand(arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "dcf", "reverse-dcf", "reverse", "wacc":
		return true
	default:
		return false
	}
}

func RunValue(parent context.Context, args []string, output, errorOutput io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		_, err := io.WriteString(output, valueUsage)
		return err
	}

	flags := flag.NewFlagSet("value", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, valueUsage)
	mode := flags.String("mode", "", "DCF model: fcf, revenue, or owner (reverse-dcf)")
	reverse := flags.Bool("reverse", false, "solve for market-implied growth (alias for reverse-dcf)")
	growth := flags.Float64("growth", 0, "annual forecast growth in percent; defaults to 3Y historical CAGR")
	discount := flags.Float64("discount", 0, "manual discount rate in percent; defaults to calculated WACC")
	terminal := flags.Float64("terminal", 2.5, "perpetual terminal growth in percent")
	years := flags.Int("years", 5, "explicit forecast period in years (1-20)")
	capexMode := flags.String("capex-mode", "raw", "CapEx forecast: raw or normalized")
	normalizeCapEx := flags.Bool("normalize-capex", false, "normalize CapEx to depreciation and amortization")
	riskFree := flags.Float64("risk-free", 4.84, "risk-free rate in percent")
	equityRiskPremium := flags.Float64("erp", 5.5, "equity risk premium in percent")
	beta := flags.Float64("beta", 1.0, "company beta")
	costOfDebt := flags.Float64("cost-debt", 5.0, "fallback pre-tax cost of debt in percent")
	taxRate := flags.Float64("tax", 21.0, "fallback tax rate in percent")
	marketCap := flags.Float64("market-cap", 0, "market capitalization override in reported currency units")
	priceFlag := flags.Float64("price", 0, "share price override in USD")
	debt := flags.Float64("debt", 0, "gross debt override in reported currency units")
	profileFlag := flags.String("profile", "", "macro profile for risk-free and tax benchmarks: us, eu, uk, ch")
	adrRatioFlag := flags.Float64("adr-ratio", 0, "ordinary shares per ADR (overrides built-in registry)")
	noLivePrice := flags.Bool("no-live-price", false, "do not fetch live market price")
	refresh := flags.Bool("refresh", false, "force fresh EDGAR, quote, and FX pulls")
	offline := flags.Bool("offline", false, "use local SQLite cache only")
	userAgent := flags.String("ua", os.Getenv("SEC_USER_AGENT"), "SEC User-Agent including contact email")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := ParseCommandFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	pos := flags.Args()
	var ticker, subcmd string
	switch len(pos) {
	case 0:
		return errors.New("provide a ticker symbol (e.g. sec value AAPL)")
	case 1:
		if isValueSubcommand(pos[0]) {
			return fmt.Errorf("provide a ticker symbol for %s (e.g. sec value AAPL %s)", pos[0], pos[0])
		}
		var err error
		ticker, err = CommandTicker([]string{pos[0]})
		if err != nil {
			return err
		}
		subcmd = "dcf"
	case 2:
		if isValueSubcommand(pos[1]) {
			var err error
			ticker, err = CommandTicker([]string{pos[0]})
			if err != nil {
				return err
			}
			subcmd = strings.ToLower(strings.TrimSpace(pos[1]))
		} else if isValueSubcommand(pos[0]) {
			subcmd = strings.ToLower(strings.TrimSpace(pos[0]))
			var err error
			ticker, err = CommandTicker([]string{pos[1]})
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("unknown value subcommand %q (choose dcf, reverse-dcf, or wacc)", pos[1])
		}
	default:
		return errors.New("value accepts TICKER and an optional subcommand (dcf, reverse-dcf, wacc)")
	}

	if *reverse || subcmd == "reverse" {
		subcmd = "reverse-dcf"
	}

	if *capexMode != "raw" && *capexMode != "normalized" {
		return errors.New("--capex-mode must be raw or normalized")
	}
	if *riskFree < 0 || *riskFree >= 100 || *equityRiskPremium < 0 || *equityRiskPremium >= 100 ||
		*costOfDebt < 0 || *costOfDebt >= 100 || *taxRate < 0 || *taxRate >= 100 || *beta < 0 || *marketCap < 0 || *priceFlag < 0 {
		return errors.New("WACC rates must be between 0 and 100 percent; beta, price, and market cap must be non-negative")
	}

	growthProvided, discountProvided, taxProvided, costDebtProvided, debtProvided, modeProvided, riskFreeProvided := false, false, false, false, false, false, false
	marketCapProvided, priceProvided := false, false
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
		case "market-cap":
			marketCapProvided = true
		case "price":
			priceProvided = true
		}
	})

	selectedMode := *mode
	if subcmd == "reverse-dcf" {
		if !modeProvided {
			selectedMode = string(valuation.DCFModeOwnerEarnings)
		}
		if selectedMode != string(valuation.DCFModeOwnerEarnings) && selectedMode != string(valuation.DCFModeFCF) && selectedMode != string(valuation.DCFModeRevenue) {
			return errors.New("reverse-dcf --mode must be owner, fcf, or revenue")
		}
		if growthProvided {
			return errors.New("--growth cannot be combined with reverse-dcf because growth is solved from market value")
		}
	} else if subcmd == "dcf" {
		if !modeProvided {
			selectedMode = string(valuation.DCFModeFCF)
		}
		if selectedMode != string(valuation.DCFModeFCF) && selectedMode != string(valuation.DCFModeRevenue) {
			return errors.New("dcf --mode must be fcf or revenue; use reverse-dcf for owner mode")
		}
	}

	if discountProvided && (*discount <= 0 || *discount >= 100) {
		return errors.New("--discount must be greater than 0 and less than 100 percent")
	}

	ctx, cancel, s, _, err := openStore(parent, *dataDir, 2*time.Minute)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.Close()

	needQuote := !*noLivePrice && !marketCapProvided && !priceProvided
	meta, err := EnsureCompanyHydrated(ctx, s, ticker, HydrateOptions{
		Refresh:     *refresh,
		Offline:     *offline,
		UserAgent:   *userAgent,
		NeedQuote:   needQuote,
		NeedFX:      true,
		ErrorOutput: errorOutput,
	})
	if err != nil {
		return err
	}

	if *adrRatioFlag > 0 {
		market.RegisterADRRatio(ticker, *adrRatioFlag)
		if info, ok := market.GetADRInfo(ticker); ok {
			meta.ADR = info
			meta.IsADR = true
		}
	}

	yearsToLoad := 5
	if subcmd == "reverse-dcf" {
		yearsToLoad = 6
	}
	summary, err := fundamentals.LoadFinancialSummary(ctx, s, yearsToLoad, ticker)
	if err != nil {
		return err
	}

	reportingCurrency := fundamentals.SummaryCurrency(summary)
	if reportingCurrency == "" {
		reportingCurrency = meta.Currency
	}
	if reportingCurrency == "" {
		reportingCurrency = "USD"
	}

	profileKey := *profileFlag
	if profileKey == "" {
		profileKey = reportingCurrency
	}
	profile := market.ResolveMacroProfile(profileKey)
	if !riskFreeProvided {
		*riskFree = profile.RiskFreeRate
	}
	if !taxProvided {
		*taxRate = profile.TaxRate
	}

	// Resolve share price in USD and market cap in reporting currency.
	resolvedPriceUSD := *priceFlag
	if resolvedPriceUSD <= 0 && meta.QuotePrice > 0 {
		resolvedPriceUSD = meta.QuotePrice
	}
	resolvedMarketCap := *marketCap
	if resolvedMarketCap <= 0 && resolvedPriceUSD > 0 {
		shares := latestOrdinaryShares(ctx, s, summary, ticker)
		if shares > 0 {
			adrShares := market.OrdinaryToADRShares(ticker, shares)
			mcapUSD := resolvedPriceUSD * adrShares
			resolvedMarketCap = market.FromUSD(mcapUSD, reportingCurrency)
		}
	}

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
		MarketCap:            resolvedMarketCap,
		DebtOverride:         debtOverride,
		UseEffectiveTaxRate:  !taxProvided,
		UseEstimatedCostDebt: !costDebtProvided,
	}

	switch subcmd {
	case "wacc":
		waccRes, err := valuation.CalculateWACC(summary, *waccConfig)
		if err != nil {
			return err
		}
		WriteFreshnessHeader(output, meta)
		return valuation.WriteWACCResult(output, summary.Company, summary.Ticker, waccRes)

	case "reverse-dcf":
		if resolvedMarketCap <= 0 {
			return errors.New("reverse-dcf requires --market-cap, --price, or a live market quote")
		}
		cfg := valuation.DCFConfig{
			Mode:                valuation.DCFMode(selectedMode),
			DiscountRate:        *discount / 100,
			TerminalRate:        *terminal / 100,
			Years:               *years,
			NormalizeCapEx:      *normalizeCapEx || *capexMode == "normalized",
			DebtOverride:        debtOverride,
			TaxRate:             *taxRate / 100,
			UseEffectiveTaxRate: !taxProvided,
			WACC:                waccConfig,
		}
		res, err := valuation.SolveReverseDCF(summary, cfg)
		if err != nil {
			return err
		}
		WriteFreshnessHeader(output, meta)
		return valuation.WriteReverseDCFResult(output, res)

	case "dcf":
		cfg := valuation.DCFConfig{
			Mode:                valuation.DCFMode(selectedMode),
			GrowthRate:          *growth / 100,
			UseHistoricalGrowth: !growthProvided,
			DiscountRate:        *discount / 100,
			TerminalRate:        *terminal / 100,
			Years:               *years,
			NormalizeCapEx:      *normalizeCapEx || *capexMode == "normalized",
			DebtOverride:        debtOverride,
			TaxRate:             *taxRate / 100,
			UseEffectiveTaxRate: !taxProvided,
			WACC:                waccConfig,
		}
		res, err := valuation.CalculateDCF(summary, cfg)
		if err != nil {
			return err
		}
		WriteFreshnessHeader(output, meta)
		if err := valuation.WriteDCFResult(output, res); err != nil {
			return err
		}
		if resolvedPriceUSD > 0 && res.ValuePerShare > 0 {
			intrinsicUSDPerADR := market.ToUSD(res.ValuePerShare, res.Currency) * market.ADRRatio(ticker)
			if intrinsicUSDPerADR > 0 {
				upside := (intrinsicUSDPerADR - resolvedPriceUSD) / resolvedPriceUSD
				mos := (intrinsicUSDPerADR - resolvedPriceUSD) / intrinsicUSDPerADR
				_, _ = fmt.Fprintf(output, "\nMarket Comparison: Current Price $%.2f USD · Intrinsic Value $%.2f USD · Margin of Safety %+.1f%% (Upside %+.1f%%)\n",
					resolvedPriceUSD, intrinsicUSDPerADR, mos*100, upside*100)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown value subcommand %q (choose dcf, reverse-dcf, or wacc)", subcmd)
	}
}

func latestOrdinaryShares(ctx context.Context, s *store.Store, summary fundamentals.FinancialSummary, ticker string) float64 {
	for _, metricName := range []string{"Shares Outstanding", "Diluted Weighted Average Shares"} {
		vals := fundamentals.NamedMetric(summary, metricName)
		for _, v := range vals {
			if fundamentals.IsShareCount(v) && v.Value > 0 {
				return v.Value
			}
		}
	}
	if shares, ok := s.LatestShareCount(ctx, ticker); ok && shares > 0 {
		return shares
	}
	return 0
}
