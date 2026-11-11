package commands

import (
	"context"
	"errors"
	"flag"
	"io"

	"sec/internal/fundamentals"
	"sec/internal/store"
)

func RunSummary(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("summary", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec summary TICKER [--years 3|4|5] [--data-dir DIR]")
	years := flags.Int("years", 5, "fiscal years to show (3-5)")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "years", "data-dir"); err != nil {
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
	databasePath := SharedDatabasePath(*dataDir)
	return WithExistingStore(parent, databasePath, ticker, func(ctx context.Context, s *store.Store) error {
		summary, err := fundamentals.LoadFinancialSummary(ctx, s, *years, ticker)
		if err != nil {
			return err
		}
		return fundamentals.WriteFinancialSummary(output, summary)
	})
}
