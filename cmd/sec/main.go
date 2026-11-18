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

Company Research
  inspect TICKER [view]       Inspect financials, quarterly/TTM, filing diffs, audits, or filings
  value TICKER [model]        Run DCF, reverse-DCF, or WACC valuation

Idea Discovery
  screen [strategy] [@univ]   Screen with rank, quality-discount, owner-yield, arbitrage,
                              gaap-arbitrage, or custom --where "<formula>" queries

Universes & Data
  universe <list|show|create> Browse, inspect, or create @-prefixed ticker universes
  data <sync|status|prune>    Manage local SEC cache, exports, and storage

Server & Shell
  serve                       Start the HTTP JSON API server
  completion <bash|zsh|fish>  Generate shell tab-completion scripts

Examples:
  sec inspect ASML
  sec inspect AAPL quarterly
  sec inspect AAPL diff --item 1A
  sec value AAPL reverse-dcf
  sec screen @sp500 --where "roic > 0.15 && fcf_yield > 0.05"
  sec screen owner-yield @euro-uk --min-yield 4

Run "sec <command> --help" for command-specific options.
`

var movedCommandHints = map[string]string{
	"summary":          "sec inspect <TICKER>",
	"read":             "sec inspect <TICKER> filings",
	"audit":            "sec inspect <TICKER> audit  (or: sec inspect audit)",
	"dcf":              "sec value <TICKER> [dcf|reverse-dcf|wacc]",
	"rank":             "sec screen rank [@universe]",
	"arbitrage":        "sec screen arbitrage [@universe]",
	"discount":         "sec screen quality-discount [@universe]",
	"quality-discount": "sec screen quality-discount [@universe]",
	"qaad":             "sec screen quality-discount [@universe]",
	"qarp":             "sec screen quality-discount [@universe]",
	"gaap-arbitrage":   "sec screen gaap-arbitrage [@universe]",
	"modern-value":     "sec screen gaap-arbitrage [@universe]",
	"modern":           "sec screen gaap-arbitrage [@universe]",
	"intangible":       "sec screen gaap-arbitrage [@universe]",
	"sync":             "sec data sync <TICKER...|@universe>  (note: inspect/value/screen now hydrate automatically)",
	"precompute":       "sec data rebuild",
	"cache":            "sec data rebuild",
	"list":             "sec data status --list",
	"export":           "sec data export <TICKER>",
}

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
	command, rest := args[0], args[1:]
	switch command {
	case "inspect":
		return commands.RunInspect(parent, rest, output, errorOutput)
	case "value":
		return commands.RunValue(parent, rest, output, errorOutput)
	case "screen":
		return commands.RunScreen(parent, rest, output, errorOutput)
	case "universe", "universes":
		return commands.RunUniverse(parent, rest, output, errorOutput)
	case "data":
		return commands.RunData(parent, rest, output, errorOutput)
	case "serve":
		return commands.RunServe(parent, rest, output, errorOutput)
	case "completion":
		return commands.RunCompletion(parent, rest, output, errorOutput)
	case "__complete":
		return commands.RunHiddenComplete(parent, rest, output, errorOutput)
	default:
		if hint, ok := movedCommandHints[command]; ok {
			return fmt.Errorf("command %q was replaced by the unified grammar; use %q (run \"sec --help\" for details)", command, hint)
		}
		return fmt.Errorf("unknown command %q\n\n%s", command, usage)
	}
}
