package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"sec/internal/edgar"
	"sec/internal/store"
)

func RunRead(parent context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("read", flag.ContinueOnError)
	SetFlagUsage(flags, errorOutput, "usage: sec read TICKER [--data-dir DIR]")
	dataDir := flags.String("data-dir", store.DefaultDirectory(), "directory containing the shared SQLite database")
	if err := ParseCommandFlags(flags, args, "data-dir"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ticker, err := CommandTicker(flags.Args())
	if err != nil {
		return err
	}
	databasePath := SharedDatabasePath(*dataDir)
	if _, err := os.Stat(databasePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no saved data for %s; run sec sync %s first", ticker, ticker)
		}
		return fmt.Errorf("inspect company database: %w", err)
	}
	return edgar.ReadFilings(parent, databasePath, ticker, os.Stdin, output)
}
