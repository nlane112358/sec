package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
)

const cacheUsage = `usage: sec cache rebuild [options]

Recompute cached summaries and screening variables for synced companies.
Run "sec cache rebuild --help" for options.
`

func RunCache(parent context.Context, args []string, output, errorOutput io.Writer) error {
	if len(args) == 0 {
		return errors.New("cache requires a subcommand: rebuild (see sec cache --help)")
	}
	if args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(output, cacheUsage)
		return err
	}
	if args[0] != "rebuild" {
		return fmt.Errorf("unknown cache command %q; choose rebuild", args[0])
	}
	return RunPrecompute(parent, args[1:], output, errorOutput)
}
