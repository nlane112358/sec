package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"sec/internal/store"
)

const completionUsage = `usage: sec completion <bash|zsh|fish>

Generate shell tab-completion script supporting commands, subcommands,
flags, cached tickers, and @-prefixed universe aliases (@sp500, @euro-uk, ...).

Setup:
  zsh:   source <(sec completion zsh)
  bash:  source <(sec completion bash)
  fish:  sec completion fish | source
`

func RunCompletion(_ context.Context, args []string, output, errorOutput io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		_, err := io.WriteString(output, completionUsage)
		return err
	}
	shell := strings.ToLower(strings.TrimSpace(args[0]))
	switch shell {
	case "bash":
		_, err := io.WriteString(output, bashCompletionScript)
		return err
	case "zsh":
		_, err := io.WriteString(output, zshCompletionScript)
		return err
	case "fish":
		_, err := io.WriteString(output, fishCompletionScript)
		return err
	default:
		return fmt.Errorf("unsupported shell %q; choose bash, zsh, or fish\n\n%s", args[0], completionUsage)
	}
}

// RunHiddenComplete serves dynamic tab-completion candidates for `sec __complete ...`.
func RunHiddenComplete(parent context.Context, args []string, output, _ io.Writer) error {
	dataDir := store.DefaultDirectory()
	var filteredArgs []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--data-dir" && i+1 < len(args) {
			dataDir = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--data-dir=") {
			dataDir = strings.TrimPrefix(args[i], "--data-dir=")
			continue
		}
		filteredArgs = append(filteredArgs, args[i])
	}

	// filteredArgs contains the words already typed on the command line (excluding binary name).
	// The last element is the current prefix being completed (possibly "").
	prefix := ""
	prior := filteredArgs
	if len(filteredArgs) > 0 {
		prefix = filteredArgs[len(filteredArgs)-1]
		prior = filteredArgs[:len(filteredArgs)-1]
	}

	candidates := computeCompletionCandidates(parent, dataDir, prior, prefix)
	for _, c := range candidates {
		if prefix == "" || strings.HasPrefix(strings.ToLower(c), strings.ToLower(prefix)) {
			_, _ = fmt.Fprintln(output, c)
		}
	}
	return nil
}

func computeCompletionCandidates(ctx context.Context, dataDir string, prior []string, prefix string) []string {
	var nonFlags []string
	var lastFlag string
	for _, w := range prior {
		if strings.HasPrefix(w, "-") {
			lastFlag = w
			continue
		}
		lastFlag = ""
		nonFlags = append(nonFlags, w)
	}

	if lastFlag == "--universe" {
		return AllUniverseAliases()
	}
	if lastFlag == "--format" {
		return []string{"table", "csv", "json"}
	}
	if lastFlag == "--item" {
		return []string{"1A", "7"}
	}
	if lastFlag == "--profile" {
		return []string{"us", "eu", "uk", "ch"}
	}

	if strings.HasPrefix(prefix, "@") {
		return AllUniverseAliases()
	}

	if len(nonFlags) == 0 {
		if strings.HasPrefix(prefix, "-") {
			return []string{"--help", "-h"}
		}
		return []string{"inspect", "value", "screen", "universe", "data", "serve", "completion"}
	}

	root := strings.ToLower(nonFlags[0])
	switch root {
	case "inspect":
		if strings.HasPrefix(prefix, "-") {
			return []string{"--years", "--quarters", "--item", "--current", "--prior", "--all", "--json", "--usd", "--gaps-only", "--form", "--limit", "--list", "--refresh", "--offline", "--data-dir"}
		}
		if len(nonFlags) == 1 {
			out := append([]string{"audit"}, cachedTickersForCompletion(ctx, dataDir)...)
			return out
		}
		return []string{"summary", "quarterly", "ttm", "diff", "audit", "filings"}

	case "value":
		if strings.HasPrefix(prefix, "-") {
			return []string{"--mode", "--growth", "--discount", "--terminal", "--years", "--capex-mode", "--market-cap", "--price", "--debt", "--beta", "--risk-free", "--erp", "--profile", "--adr-ratio", "--no-live-price", "--refresh", "--offline", "--data-dir"}
		}
		if len(nonFlags) == 1 {
			return cachedTickersForCompletion(ctx, dataDir)
		}
		return []string{"dcf", "reverse-dcf", "wacc"}

	case "screen":
		if strings.HasPrefix(prefix, "-") {
			return []string{"--where", "--universe", "--sort", "--asc", "--top", "--columns", "--format", "--prices", "--min-score", "--min-roic", "--min-discount", "--min-yield", "--min-adj-roic", "--hurdle", "--surging", "--refresh", "--offline", "--workers", "--data-dir"}
		}
		var out []string
		hasStrategy := false
		for _, w := range nonFlags[1:] {
			if _, ok := canonicalScreenStrategy(w); ok {
				hasStrategy = true
				break
			}
		}
		if !hasStrategy {
			out = append(out, "rank", "quality-discount", "owner-yield", "arbitrage", "gaap-arbitrage")
		}
		out = append(out, AllUniverseAliases()...)
		return out

	case "universe":
		if strings.HasPrefix(prefix, "-") {
			return []string{"--synced-only", "--missing-only", "--tickers", "--file", "--description", "--force", "--data-dir"}
		}
		if len(nonFlags) == 1 {
			return []string{"list", "show", "create"}
		}
		if nonFlags[1] == "show" || nonFlags[1] == "inspect" {
			return AllUniverseAliases()
		}
		return cachedTickersForCompletion(ctx, dataDir)

	case "data":
		if strings.HasPrefix(prefix, "-") {
			return []string{"--workers", "--skip-existing", "--refresh", "--older-than", "--quotes", "--fx", "--filings", "--orphans", "--dry-run", "--list", "--format", "--data-dir"}
		}
		if len(nonFlags) == 1 {
			return []string{"sync", "status", "prune", "export", "rebuild"}
		}
		if nonFlags[1] == "sync" {
			out := append([]string(nil), AllUniverseAliases()...)
			out = append(out, cachedTickersForCompletion(ctx, dataDir)...)
			return out
		}
		if nonFlags[1] == "export" {
			return cachedTickersForCompletion(ctx, dataDir)
		}
		return nil

	case "serve":
		return []string{"--port", "--addr", "--data-dir"}

	case "completion":
		return []string{"bash", "zsh", "fish"}
	}
	return nil
}

func cachedTickersForCompletion(ctx context.Context, dataDir string) []string {
	dbPath := SharedDatabasePath(dataDir)
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	qCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	s, err := store.Open(qCtx, dbPath)
	if err != nil {
		return nil
	}
	defer s.Close()
	companies, err := s.Companies(qCtx)
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(companies))
	out := make([]string, 0, len(companies))
	for _, c := range companies {
		if _, ok := seen[c.Ticker]; !ok {
			seen[c.Ticker] = struct{}{}
			out = append(out, c.Ticker)
		}
	}
	sort.Strings(out)
	return out
}

const bashCompletionScript = `# bash completion for sec
_sec_complete() {
    local cur prev
    cur="${COMP_WORDS[COMP_CWORD]}"
    local args=("${COMP_WORDS[@]:1:COMP_CWORD-1}")
    local completions
    completions=$(sec __complete "${args[@]}" "$cur" 2>/dev/null)
    COMPREPLY=( $(compgen -W "$completions" -- "$cur") )
    return 0
}
complete -o default -F _sec_complete sec
`

const zshCompletionScript = `#compdef sec
_sec() {
    local -a args completions
    args=("${words[@]:1:$((CURRENT-1))}")
    completions=(${(f)"$(sec __complete "${args[@]}" "${words[CURRENT]}" 2>/dev/null)"})
    if (( ${#completions} > 0 )); then
        compadd -Q -- "${completions[@]}"
    fi
}
compdef _sec sec
`

const fishCompletionScript = `# fish completion for sec
function __sec_complete
    set -l tokens (commandline -opc)
    set -e tokens[1]
    set -l cur (commandline -ct)
    sec __complete $tokens $cur 2>/dev/null
end
complete -c sec -f -a '(__sec_complete)'
`

