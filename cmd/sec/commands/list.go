package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"sec/internal/store"
)

func RunList(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec list [--data-dir DIR]")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(flags.Args()) != 0 {
		return errors.New("list does not take positional arguments")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	s, err := store.Open(ctx, SharedDatabasePath(*dataDir))
	if err != nil {
		return err
	}
	defer s.Close()
	companies, err := s.Companies(ctx)
	if err != nil {
		return err
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "TICKER\tCOMPANY\tCIK"); err != nil {
		return err
	}
	for _, company := range companies {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\n", company.Ticker, company.Name, company.CIK); err != nil {
			return err
		}
	}
	return writer.Flush()
}
