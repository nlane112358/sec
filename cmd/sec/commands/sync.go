package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"sec/internal/edgar"
	"sec/internal/store"
)

func RunSync(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec sync TICKER... [--workers 1-4] [--data-dir DIR] [--ua STRING]\n       sec sync --universe FILE [--workers 1-4] [--data-dir DIR] [--ua STRING] [--skip-existing]")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	userAgent := flags.String("ua", os.Getenv("SEC_USER_AGENT"), "SEC User-Agent including contact email")
	universeFile := flags.String("universe", "", "file containing one ticker per line")
	workers := flags.Int("workers", 4, "concurrent sync workers (1-4)")
	skipExisting := flags.Bool("skip-existing", false, "skip companies already present in the database")
	if err := ParseCommandFlags(flags, args, "data-dir", "ua", "universe", "workers", "skip-existing"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	tickers, err := LoadTickerUniverse(flags.Args(), *universeFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*userAgent) == "" {
		return errors.New("provide --ua 'Research App contact@example.com' or set SEC_USER_AGENT")
	}
	if *workers < 1 || *workers > 4 {
		return errors.New("--workers must be between 1 and 4")
	}
	databasePath := SharedDatabasePath(*dataDir)
	s, err := store.Open(parent, databasePath)
	if err != nil {
		return err
	}
	defer s.Close()
	client := edgar.NewClient(*userAgent)
	if len(tickers) == 0 {
		return nil
	}
	workerCount := min(*workers, len(tickers))
	var writeMu sync.Mutex
	safeOutput := LockedWriter{Mu: &writeMu, Writer: output}
	safeErrorOutput := LockedWriter{Mu: &writeMu, Writer: errorOutput}
	_, _ = fmt.Fprintf(safeErrorOutput, "Syncing %d companies with %d workers; SEC requests share the 8.5 requests/second limiter.\n", len(tickers), workerCount)
	type syncJob struct {
		index  int
		ticker string
	}
	jobs := make(chan syncJob, len(tickers))
	failures := make([]error, len(tickers))
	for index, ticker := range tickers {
		jobs <- syncJob{index: index, ticker: ticker}
	}
	close(jobs)
	var workersDone sync.WaitGroup
	workersDone.Add(workerCount)
	for range workerCount {
		go func() {
			defer workersDone.Done()
			for job := range jobs {
				if err := parent.Err(); err != nil {
					failures[job.index] = err
					continue
				}
				if *skipExisting {
					if _, _, _, errExist := s.CompanyForTicker(parent, job.ticker); errExist == nil {
						fmt.Fprintf(safeErrorOutput, "[%d/%d] Skipping %s (already synced)\n", job.index+1, len(tickers), job.ticker)
						continue
					}
				}
				ctx, cancel := context.WithTimeout(parent, 90*time.Second)
				err := SyncCompany(ctx, client, s, job.ticker, job.index+1, len(tickers), databasePath, safeOutput, safeErrorOutput)
				cancel()
				if err != nil {
					fmt.Fprintf(safeErrorOutput, "Sync failed for %s: %v\n", job.ticker, err)
					failures[job.index] = fmt.Errorf("%s: %w", job.ticker, err)
				}
			}
		}()
	}
	workersDone.Wait()
	if err := parent.Err(); err != nil {
		return err
	}
	return errors.Join(failures...)
}

func SyncCompany(ctx context.Context, client *edgar.Client, s *store.Store, ticker string, index, count int, databasePath string, output, errorOutput io.Writer) error {
	if _, err := fmt.Fprintf(errorOutput, "[%d/%d] Resolving %s…\n", index, count, ticker); err != nil {
		return err
	}
	cik, err := client.ResolveTicker(ctx, ticker)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(errorOutput, "[%d/%d] Fetching filing history for %s…\n", index, count, ticker); err != nil {
		return err
	}
	submissions, err := client.Submissions(ctx, cik)
	if err != nil {
		return fmt.Errorf("fetch SEC filing submissions: %w", err)
	}
	if _, err := fmt.Fprintf(errorOutput, "[%d/%d] Fetching company facts for %s…\n", index, count, ticker); err != nil {
		return err
	}
	factsData, err := client.Facts(ctx, cik)
	if err != nil {
		return fmt.Errorf("fetch SEC company facts: %w", err)
	}
	facts := edgar.ExtractFacts(factsData, cik)
	filings := submissions.AllFilings(cik)
	companyName := submissions.Name
	if strings.TrimSpace(companyName) == "" {
		companyName = factsData.Entity
	}
	if _, err := fmt.Fprintf(errorOutput, "[%d/%d] Saving %d facts and %d filings for %s…\n", index, count, len(facts), len(filings), ticker); err != nil {
		return err
	}
	if err := s.SaveCompany(ctx, cik, companyName, ticker, string(submissions.SIC), submissions.SICDescription); err != nil {
		return err
	}
	if err := s.SaveFacts(ctx, ticker, facts); err != nil {
		return err
	}
	if err := s.SaveFilings(ctx, filings); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "saved_facts=%d saved_filings=%d ticker=%s cik=%s database=%s\n", len(facts), len(filings), ticker, cik, databasePath)
	return err
}
