package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"sec/internal/fundamentals"
	"sec/internal/screener"
	"sec/internal/store"
)

func RunPrecompute(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("precompute", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec cache rebuild [TICKER...] [--universe FILE] [--force] [--workers N] [--data-dir DIR]")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	universeFile := flags.String("universe", "", "file containing one ticker per line")
	force := flags.Bool("force", false, "recompute variables even for clean up-to-date companies")
	workers := flags.Int("workers", 4, "concurrent compute workers (1-8)")
	if err := ParseCommandFlags(flags, args, "data-dir", "universe", "workers"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *workers < 1 || *workers > 8 {
		return errors.New("--workers must be between 1 and 8")
	}
	var tickers []string
	if *universeFile != "" || len(flags.Args()) > 0 {
		loaded, err := LoadTickerUniverse(flags.Args(), *universeFile)
		if err != nil {
			return err
		}
		tickers = loaded
	}

	databasePath := SharedDatabasePath(*dataDir)
	return WithExistingStoreTimeout(parent, databasePath, "", 60*time.Minute, func(ctx context.Context, s *store.Store) error {
		before, err := s.PrecomputeStats(ctx, fundamentals.SummarySchemaVersion, screener.RankSchemaVersion, screener.GAAPArbitrageSchemaVersion)
		if err != nil {
			return err
		}
		fmt.Fprintf(errorOutput, "Database companies=%d (clean=%d dirty=%d missing=%d)\n",
			before.TotalCompanies, before.CleanSummaries, before.DirtySummaries, before.MissingSummaries)

		lastLogged := 0
		report, err := screener.PrecomputeMissingOrDirty(ctx, s, screener.PrecomputeOptions{
			Tickers:           tickers,
			RankYears:         screener.DefaultPrecomputeRankYears,
			ArbYears:          screener.DefaultPrecomputeArbYears,
			Force:             *force,
			Workers:           *workers,
			RefreshRankScores: true,
			OnProgress: func(p screener.PrecomputeProgress) {
				if p.Completed == p.Total || p.Completed-lastLogged >= 250 {
					lastLogged = p.Completed
					fmt.Fprintf(errorOutput, "[%d/%d] Precomputed %s (CIK %s)\n", p.Completed, p.Total, p.Ticker, p.CIK)
				}
			},
		})
		if err != nil {
			return err
		}

		after, err := s.PrecomputeStats(ctx, fundamentals.SummarySchemaVersion, screener.RankSchemaVersion, screener.GAAPArbitrageSchemaVersion)
		if err != nil {
			return err
		}

		_, err = fmt.Fprintf(output,
			"precomputed=%d stale_checked=%d rank_scores_updated=%d clean_summaries=%d rank_5y=%d rank_6y=%d gaap_arb_5y=%d duration=%s\n",
			report.Recomputed, report.CheckedStale, report.ScoresUpdated,
			after.CleanSummaries, after.Rank5Count, after.Rank6Count, after.Arb5Count,
			report.Duration.Round(time.Millisecond),
		)
		return err
	})
}
