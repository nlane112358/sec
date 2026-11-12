package commands

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"

	"sec/internal/fundamentals"
	"sec/internal/store"
)

func RunSummary(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("summary", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec summary TICKER [--years 3-5] [--quarterly | -q] [--quarters 1-16] [--ttm] [--data-dir DIR]")
	years := flags.Int("years", 5, "fiscal years to show (3-5)")
	quarterly := flags.Bool("quarterly", false, "display quarterly financial metrics (10-Q)")
	qShort := flags.Bool("q", false, "display quarterly financial metrics (short alias)")
	quarters := flags.Int("quarters", 8, "number of quarters to show (1-16)")
	ttm := flags.Bool("ttm", false, "display Trailing-Twelve-Month (TTM) data and quarterly inflections")
	view := flags.String("view", "", "view mode: annual, quarterly, or ttm")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "years", "quarters", "view", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := CommandTicker(flags.Args())
	if err != nil {
		return err
	}
	if *years < 3 || *years > 5 {
		return errors.New("--years must be between 3 and 5")
	}
	if *quarters < 1 || *quarters > 16 {
		return errors.New("--quarters must be between 1 and 16")
	}

	isQuarterly := *quarterly || *qShort || strings.EqualFold(*view, "quarterly")
	isTTM := *ttm || strings.EqualFold(*view, "ttm")

	databasePath := SharedDatabasePath(*dataDir)
	return WithExistingStore(parent, databasePath, ticker, func(ctx context.Context, s *store.Store) error {
		if isQuarterly {
			summary, err := fundamentals.LoadQuarterlySummary(ctx, s, *quarters, ticker)
			if err != nil {
				return err
			}
			return fundamentals.WriteFinancialSummary(output, summary)
		}
		if isTTM {
			summary, err := fundamentals.LoadTTMFinancialSummary(ctx, s, ticker)
			if err != nil {
				return err
			}
			if err := fundamentals.WriteFinancialSummary(output, summary); err != nil {
				return err
			}
			ttmDetails, err := fundamentals.LoadTTMSummary(ctx, s, ticker)
			if err == nil {
				_ = fundamentals.WriteTTMInflections(output, ttmDetails.Inflections)
			}
			return nil
		}
		summary, err := fundamentals.LoadFinancialSummary(ctx, s, *years, ticker)
		if err != nil {
			return err
		}
		return fundamentals.WriteFinancialSummary(output, summary)
	})
}
