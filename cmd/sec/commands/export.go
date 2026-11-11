package commands

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"sec/internal/store"
)

func RunExport(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec export TICKER [--format csv|json] [--data-dir DIR]")
	format := flags.String("format", "csv", "export format: csv or json")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "format", "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := CommandTicker(flags.Args())
	if err != nil {
		return err
	}
	if *format != "csv" && *format != "json" {
		return errors.New("--format must be csv or json")
	}
	databasePath := SharedDatabasePath(*dataDir)
	return WithExistingStore(parent, databasePath, ticker, func(ctx context.Context, s *store.Store) error {
		facts, err := s.LoadFacts(ctx, ticker)
		if err != nil {
			return err
		}
		if *format == "json" {
			encoder := json.NewEncoder(output)
			encoder.SetIndent("", "  ")
			return encoder.Encode(facts)
		}
		writer := csv.NewWriter(output)
		if err := writer.Write([]string{"company", "cik", "ticker", "taxonomy", "concept", "label", "unit", "value", "period_start", "period_end", "filing_date", "form", "accession_number", "frame"}); err != nil {
			return err
		}
		for _, fact := range facts {
			if err := writer.Write([]string{
				fact.Company, fact.CIK, fact.Ticker, fact.Taxonomy, fact.Concept, fact.Label,
				fact.Unit, fmt.Sprintf("%g", fact.Value), fact.Start, fact.End, fact.FilingDate,
				fact.Form, fact.Accession, fact.Frame,
			}); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	})
}
