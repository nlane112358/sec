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

Data
  sync TICKER...              Fetch company facts and filings
  universe list|status        Browse universes and sync coverage

Research
  summary TICKER              Show financial metrics
  dcf TICKER                  Estimate intrinsic value
  screen MODEL                Rank or screen companies
  read TICKER                 Browse saved filings

Output & maintenance
  export TICKER               Export facts as CSV or JSON
  audit [TICKER]              Diagnose missing facts or company tags
  cache rebuild               Recompute derived data

Screen models: quality, growth, discount, intangible
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
	case "precompute":
		// Retain the original command as a compatibility alias.
		return commands.RunPrecompute(parent, args, output, errorOutput)
	case "cache":
		return commands.RunCache(parent, args, output, errorOutput)
	case "summary":
		return commands.RunSummary(parent, args, output, errorOutput)
	case "dcf":
		return commands.RunDCF(parent, args, output, errorOutput)
	case "screen":
		return commands.RunScreen(parent, args, output, errorOutput)
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
	case "discount", "quality-discount", "qaad", "qarp":
		return commands.RunQualityDiscount(parent, args, output, errorOutput)
	case "gaap-arbitrage", "modern-value", "modern", "intangible":
		return commands.RunModernGAAPArbitrage(parent, args, output, errorOutput)
	case "audit":
		return commands.RunAudit(parent, args, output, errorOutput)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, usage)
	}
}
