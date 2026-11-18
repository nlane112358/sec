package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRootHelpShowsMentalModelGrammar(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := run(context.Background(), []string{"--help"}, &out, &errOut); err != nil {
		t.Fatalf("run --help failed: %v", err)
	}
	help := out.String()
	for _, want := range []string{"inspect ", "value ", "screen ", "universe ", "data ", "serve", "completion "} {
		if !strings.Contains(help, want) {
			t.Errorf("expected root help to contain %q, got:\n%s", want, help)
		}
	}
	for _, flat := range []string{"  sync ", "  precompute ", "  cache ", "  summary ", "  dcf ", "  read ", "  rank ", "  arbitrage "} {
		if strings.Contains(help, flat) {
			t.Errorf("root help should not advertise legacy flat command %q", flat)
		}
	}
}

func TestLegacyCommandsProvideMigrationHints(t *testing.T) {
	cases := map[string]string{
		"summary":        "sec inspect",
		"read":           "sec inspect",
		"audit":          "sec inspect",
		"dcf":            "sec value",
		"rank":           "sec screen rank",
		"gaap-arbitrage": "sec screen gaap-arbitrage",
		"sync":           "sec data sync",
		"precompute":     "sec data rebuild",
		"cache":          "sec data rebuild",
		"export":         "sec data export",
	}
	for cmd, hint := range cases {
		var out, errOut bytes.Buffer
		err := run(context.Background(), []string{cmd}, &out, &errOut)
		if err == nil {
			t.Fatalf("expected legacy command %q to return migration error", cmd)
		}
		if !strings.Contains(err.Error(), hint) {
			t.Errorf("legacy command %q error = %q, want hint containing %q", cmd, err.Error(), hint)
		}
	}
}
