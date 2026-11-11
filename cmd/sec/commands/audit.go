package commands

import (
	"context"
	"errors"
	"flag"
	"io"
	"time"

	"sec/internal/fundamentals"
	"sec/internal/store"
)

func RunAudit(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("audit", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec audit [TICKER] [--gaps-only] [--data-dir DIR]")
	gapsOnly := flags.Bool("gaps-only", false, "only list companies that have missing data")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")

	if err := ParseCommandFlags(flags, args, "gaps-only", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	databasePath := SharedDatabasePath(*dataDir)
	positionals := flags.Args()

	// If a specific ticker is passed: run deep diagnostic
	if len(positionals) == 1 {
		ticker, err := CommandTicker(positionals)
		if err != nil {
			return err
		}
		return WithExistingStore(parent, databasePath, ticker, func(ctx context.Context, s *store.Store) error {
			return fundamentals.DiagnoseTicker(ctx, s, ticker, output)
		})
	}
	if len(positionals) > 1 {
		return errors.New("audit accepts at most one ticker")
	}

	// Full universe scan
	return WithExistingStoreTimeout(parent, databasePath, "", 5*time.Minute, func(ctx context.Context, s *store.Store) error {
		results, err := fundamentals.AuditDatabase(ctx, s)
		if err != nil {
			return err
		}
		return fundamentals.WriteAuditSummary(output, results, *gapsOnly)
	})
}
