package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"sec/internal/api"
	"sec/internal/market"
	"sec/internal/screener"
	"sec/internal/store"
)

const screenUsage = `usage: sec screen [strategy] [@universe|TICKER...] [options]
       sec screen [@universe|TICKER...] --where "<formula>" [options]

Strategies:
  rank              Rank companies within SIC peer groups on multi-year quality
  quality-discount  Quality-at-a-Discount (QaaD): high-ROIC compounders below DCF value
  owner-yield       Intangible-adjusted Owner Earnings Yield ((FCF - SBC + Intangibles) / MCap)
  arbitrage         Expectations arbitrage: historical/peer growth vs reverse-DCF implied growth
  gaap-arbitrage    Peters & Taylor / Damodaran intangible-capitalized GAAP arbitrage

Formula Queries (--where):
  Filter any universe or local database using arithmetic and boolean expressions:
    Metrics: roic, adj_roic, gaap_roic, roe, fcf_yield, owner_yield, pe, pb, ev_fcf,
             ev_ebit, market_cap, ev, price, revenue, operating_income, net_income,
             fcf, ocf, capex, sbc, cash, debt, net_debt, equity, gross_margin,
             operating_margin, fcf_margin, revenue_cagr, eps_growth, share_reduction,
             score, sector, currency
    Literals: 0.15 or 15%, 10B / 500M / 250K / 1T, "Technology", true/false
    Ops:      && / AND, || / OR, ! / NOT, >, >=, <, <=, ==, !=, +, -, *, /, has(), abs()

Common Options:
  @UNIVERSE / --universe   Universe alias (@sp500, @nasdaq100, @euro-uk, @defense, @adr) or file
  --where EXPR             Filter candidates using formula expression
  --top N                  Limit output to top N results
  --prices FILE            Optional CSV/TSV/JSON valuation override file
  --refresh                Force fresh EDGAR, quote, and FX pulls
  --offline                Use local SQLite cache only
  --workers N              Concurrent EDGAR hydration workers (1-4, default 4)
  --data-dir DIR           Directory containing the shared SQLite database

Examples:
  sec screen @sp500 --where "roic > 0.15 && fcf_yield > 0.05"
  sec screen rank @nasdaq100 --top 20
  sec screen owner-yield @euro-uk --min-yield 4
  sec screen quality-discount @sp500 --min-discount 15
  sec screen gaap-arbitrage @nasdaq100 --surging
`

var screenValueFlags = map[string]bool{
	"universe": true, "where": true, "sort": true, "top": true, "years": true,
	"min-revenue": true, "currency": true, "prices": true, "min-score": true,
	"min-roic": true, "min-discount": true, "min-fcf-yield": true, "min-consistency": true,
	"min-yield": true, "min-adj-roic": true, "max-gaap-roic": true, "max-implied-growth": true,
	"growth": true, "discount": true, "terminal": true, "risk-free": true, "erp": true,
	"profile": true, "columns": true, "format": true, "period": true, "workers": true,
	"ua": true, "data-dir": true,
}

func canonicalScreenStrategy(arg string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "rank", "quality":
		return "rank", true
	case "quality-discount", "discount", "qaad", "qarp":
		return "quality-discount", true
	case "owner-yield", "yield":
		return "owner-yield", true
	case "arbitrage", "growth", "expectations":
		return "arbitrage", true
	case "gaap-arbitrage", "intangible", "modern", "modern-value":
		return "gaap-arbitrage", true
	default:
		return "", false
	}
}

func RunScreen(parent context.Context, args []string, output, errorOutput io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		_, err := io.WriteString(output, screenUsage)
		return err
	}

	strategy, strategyArgs, hasWhere := extractScreenStrategy(args)
	if strategy != "" {
		switch strategy {
		case "rank":
			return RunRank(parent, strategyArgs, output, errorOutput)
		case "quality-discount":
			return RunQualityDiscount(parent, strategyArgs, output, errorOutput)
		case "owner-yield":
			return RunOwnerYield(parent, strategyArgs, output, errorOutput)
		case "arbitrage":
			return RunArbitrage(parent, strategyArgs, output, errorOutput)
		case "gaap-arbitrage":
			return RunModernGAAPArbitrage(parent, strategyArgs, output, errorOutput)
		}
	}

	if hasWhere {
		return RunFormulaScreen(parent, args, output, errorOutput)
	}

	// If positional starts with @universe (without strategy or --where), default to rank.
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			name := strings.TrimLeft(arg, "-")
			if before, _, found := strings.Cut(name, "="); found {
				name = before
			}
			if screenValueFlags[name] && !strings.Contains(arg, "=") {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "@") {
			return RunRank(parent, args, output, errorOutput)
		}
		return fmt.Errorf("unknown screen strategy %q; choose rank, quality-discount, owner-yield, arbitrage, gaap-arbitrage, or pass --where \"<formula>\"", arg)
	}

	return errors.New("screen requires a strategy (rank, quality-discount, owner-yield, arbitrage, gaap-arbitrage) or --where \"<formula>\"")
}

func extractScreenStrategy(args []string) (strategy string, remaining []string, hasWhere bool) {
	remaining = make([]string, 0, len(args))
	foundStrategy := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			remaining = append(remaining, args[i:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			remaining = append(remaining, arg)
			name := strings.TrimLeft(arg, "-")
			if before, _, found := strings.Cut(name, "="); found {
				name = before
			}
			if name == "where" {
				hasWhere = true
			}
			if screenValueFlags[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
				remaining = append(remaining, args[i])
			}
			continue
		}
		if !foundStrategy {
			if strat, ok := canonicalScreenStrategy(arg); ok {
				strategy = strat
				foundStrategy = true
				continue
			}
		}
		remaining = append(remaining, arg)
	}
	return strategy, remaining, hasWhere
}

func prepareScreenUniverse(
	ctx context.Context,
	s *store.Store,
	posArgs []string,
	universeFlag string,
	opts HydrateOptions,
) (tickers []string, universeLabel string, batch BatchHydrationResult, err error) {
	if universeFlag != "" && len(posArgs) > 0 {
		return nil, "", batch, errors.New("provide tickers/@universe positionally or via --universe, not both")
	}

	if universeFlag != "" {
		tickers, err = LoadTickerUniverse(nil, universeFlag)
		if err != nil {
			return nil, "", batch, err
		}
		if _, alias, ok := resolveUniverseSpec(universeFlag); ok {
			universeLabel = alias
		} else {
			universeLabel = universeFlag
		}
	} else if len(posArgs) > 0 {
		tickers, err = LoadTickerUniverse(posArgs, "")
		if err != nil {
			return nil, "", batch, err
		}
		if len(posArgs) == 1 && strings.HasPrefix(strings.TrimSpace(posArgs[0]), "@") {
			if _, alias, ok := resolveUniverseSpec(posArgs[0]); ok {
				universeLabel = alias
			} else {
				universeLabel = posArgs[0]
			}
		} else {
			universeLabel = fmt.Sprintf("%d tickers", len(tickers))
		}
	}

	if len(tickers) > 0 {
		batch, err = HydrateTickersBatch(ctx, s, tickers, opts)
		return tickers, universeLabel, batch, err
	}

	// Screening entire existing database.
	companies, err := s.Companies(ctx)
	if err != nil {
		return nil, "", batch, err
	}
	if len(companies) == 0 {
		return nil, "", batch, errors.New("no companies in local database yet; pass a @universe (e.g. sec screen @sp500 --where \"...\") or tickers")
	}
	batch.Total = len(companies)
	batch.CacheHits = len(companies)
	batch.Currencies = make(map[string]FXHydrationInfo)

	_, _ = screener.PrecomputeMissingOrDirty(ctx, s, screener.PrecomputeOptions{
		RankYears:         screener.DefaultPrecomputeRankYears,
		ArbYears:          screener.DefaultPrecomputeArbYears,
		RefreshRankScores: true,
	})
	if fxRates, fxErr := s.LoadAllFXRates(ctx); fxErr == nil {
		for _, fx := range fxRates {
			if fx.RateToUSD > 0 {
				market.SetFXRate(fx.Currency, fx.RateToUSD)
			}
		}
	}
	// Ensure FX rates for any non-USD currencies stored in summaries.
	rows, qErr := s.DB().QueryContext(ctx, `SELECT DISTINCT UPPER(currency) FROM sec_company_summaries WHERE currency <> '' AND UPPER(currency) <> 'USD'`)
	if qErr == nil {
		var curs []string
		for rows.Next() {
			var cur string
			if rows.Scan(&cur) == nil && cur != "" {
				curs = append(curs, cur)
			}
		}
		rows.Close()
		for _, cur := range curs {
			fxInfo := ensureCurrencyFX(ctx, s, cur, opts, nil)
			batch.Currencies[cur] = fxInfo
			if fxInfo.Source == "live" {
				batch.FXLiveCount++
			}
		}
	}
	return nil, "", batch, nil
}

// filterTickersByWhere evaluates a --where formula on a candidate ticker subset
// so any screening strategy can compose with custom formula filters.
func filterTickersByWhere(
	ctx context.Context,
	s *store.Store,
	whereExpr string,
	candidateTickers []string,
	prices map[string]float64,
	marketCaps map[string]float64,
	opts HydrateOptions,
) (map[string]bool, error) {
	if strings.TrimSpace(whereExpr) == "" || len(candidateTickers) == 0 {
		return nil, nil
	}
	res, err := api.ScreenByFormula(ctx, s, api.FormulaScreenOptions{
		Tickers:       candidateTickers,
		Where:         whereExpr,
		MarketCaps:    marketCaps,
		Prices:        prices,
		Offline:       opts.Offline,
		RefreshQuotes: opts.Refresh,
		OnQuoteHydrate: func(qCtx context.Context, missing []string) map[string]float64 {
			hydrated, _ := EnsureUniverseQuotes(qCtx, s, missing, prices, marketCaps, opts)
			return hydrated
		},
	})
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(res.Candidates))
	for _, c := range res.Candidates {
		allowed[c.Ticker] = true
	}
	return allowed, nil
}

func validateScreenWorkers(workers int) error {
	if workers < 1 || workers > 4 {
		return errors.New("--workers must be between 1 and 4")
	}
	return nil
}

func defaultScreenTimeout() time.Duration {
	return 60 * time.Minute
}

func registerFlagCheck(flags *flag.FlagSet, names ...string) map[string]bool {
	set := make(map[string]bool, len(names))
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	flags.Visit(func(f *flag.Flag) {
		if wanted[f.Name] {
			set[f.Name] = true
		}
	})
	return set
}
