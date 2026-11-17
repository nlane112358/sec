package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
)

const screenUsage = `usage: sec screen MODEL [options]

Models:
  quality      Rank companies on business quality
  growth       Compare historical growth with market-implied growth
  discount     Find quality companies trading below estimated value
  intangible   Screen for intangible-capitalized value

Run "sec screen MODEL --help" for model-specific options.
`

func RunScreen(parent context.Context, args []string, output, errorOutput io.Writer) error {
	if len(args) == 0 {
		return errors.New("screen requires a model: quality, growth, discount, or intangible (see sec screen --help)")
	}
	if args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(output, screenUsage)
		return err
	}

	model, modelArgs := args[0], args[1:]
	switch model {
	case "quality", "rank":
		return RunRank(parent, modelArgs, output, errorOutput)
	case "growth", "arbitrage":
		return RunArbitrage(parent, modelArgs, output, errorOutput)
	case "discount":
		return RunQualityDiscount(parent, modelArgs, output, errorOutput)
	case "intangible", "gaap-arbitrage", "modern", "modern-value":
		return RunModernGAAPArbitrage(parent, modelArgs, output, errorOutput)
	default:
		return fmt.Errorf("unknown screen model %q; choose quality, growth, discount, or intangible", model)
	}
}
