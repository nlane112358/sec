package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"sec/cmd/sec/commands"
)

const usage = `usage: sec <command> [arguments] [options]

Commands:
  sync TICKER...              Download or refresh facts and filings
  sync --universe FILE        Sync newline-separated tickers from a file
  summary TICKER [--years N]  Show annual financial metrics (3-5 years)
  dcf TICKER [options]        Estimate intrinsic value from SEC facts
  arbitrage [options]         Rank quality compounders against implied growth
  gaap-arbitrage [options]    Screen modern GAAP arbitrage (intangible-capitalized value)
  audit [--gaps-only]         Scan the database and report missing facts
  audit TICKER                Deep-dive diagnostic on a company to discover raw tags
  read TICKER                 Browse and read saved filings
  list                        List companies in the shared database
  universe [list]             List available universes and sync coverage
  export TICKER --format FMT  Export saved facts as csv or json
  rank [--universe FILE]      Screen and rank the saved universe
  serve [--port PORT]         Start web server and interactive frontend

Options:
  --data-dir DIR  directory containing sec.db (default SEC_DATABASE_DIR or data)

Sync option:
  --ua STRING     SEC User-Agent including contact email (default SEC_USER_AGENT)
  --workers N     concurrent sync workers (1-4, default 4)

Run "sec <command> --help" for command-specific options.
`

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

func run(parent context.Context, args []string, output, errorOutput io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(output, usage)
		return err
	}
	command, args := args[0], args[1:]
	switch command {
	case "sync":
		return commands.RunSync(parent, args, output, errorOutput)
	case "summary":
		return commands.RunSummary(parent, args, output, errorOutput)
	case "dcf":
		return commands.RunDCF(parent, args, output, errorOutput)
	case "read":
		return commands.RunRead(parent, args, output, errorOutput)
	case "list":
		return commands.RunList(parent, args, output, errorOutput)
	case "universe", "universes":
		return commands.RunUniverse(parent, args, output, errorOutput)
	case "export":
		return commands.RunExport(parent, args, output, errorOutput)
	case "rank":
		return commands.RunRank(parent, args, output, errorOutput)
	case "arbitrage":
		return commands.RunArbitrage(parent, args, output, errorOutput)
	case "gaap-arbitrage", "modern-value", "modern", "intangible":
		return commands.RunModernGAAPArbitrage(parent, args, output, errorOutput)
	case "audit":
		return commands.RunAudit(parent, args, output, errorOutput)
	case "serve":
		return commands.RunServe(parent, args, output, errorOutput)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, usage)
	}
}
