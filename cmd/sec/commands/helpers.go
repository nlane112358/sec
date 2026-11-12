package commands

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"sec/internal/store"
)

var tickerPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{0,14}$`)

// LockedWriter wraps an io.Writer with a mutex for safe concurrent writes.
type LockedWriter struct {
	Mu     *sync.Mutex
	Writer io.Writer
}

func (w LockedWriter) Write(data []byte) (int, error) {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	return w.Writer.Write(data)
}

func SetFlagUsage(flags *flag.FlagSet, errorOutput io.Writer, usageLine string) {
	flags.SetOutput(errorOutput)
	flags.Usage = func() {
		fmt.Fprintln(errorOutput, usageLine)
		flags.PrintDefaults()
	}
}

func WithExistingStore(parent context.Context, path, ticker string, action func(context.Context, *store.Store) error) error {
	return WithExistingStoreTimeout(parent, path, ticker, 15*time.Second, action)
}

func WithExistingStoreTimeout(parent context.Context, path, ticker string, timeout time.Duration, action func(context.Context, *store.Store) error) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if ticker == "" {
				return errors.New("no shared SEC database yet; sync a ticker or ticker universe first")
			}
			return fmt.Errorf("no saved data for %s; run sec sync %s first", ticker, ticker)
		}
		return fmt.Errorf("inspect company database: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	s, err := store.Open(ctx, path)
	if err != nil {
		return err
	}
	defer s.Close()
	return action(ctx, s)
}

func SharedDatabasePath(dataDir string) string {
	return filepath.Join(dataDir, "sec.db")
}

func resolveUniversePath(path string) string {
	candidates := []string{
		path,
		path + ".txt",
		filepath.Join("universes", path),
		filepath.Join("universes", path + ".txt"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return path
}

func LoadTickerUniverse(args []string, path string) ([]string, error) {
	if path != "" && len(args) != 0 {
		return nil, errors.New("provide tickers or --universe FILE, not both")
	}
	rawTickers := append([]string(nil), args...)
	if path != "" {
		filePath := resolveUniversePath(path)
		file, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("open ticker universe: %w", err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.ContainsAny(line, ",\t") {
				fields := strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == '\t' })
				if len(fields) == 0 {
					continue
				}
				line = strings.TrimSpace(fields[0])
				line = strings.Trim(line, "\"'")
			}
			if strings.EqualFold(line, "ticker") || strings.EqualFold(line, "symbol") {
				continue
			}
			rawTickers = append(rawTickers, strings.Fields(line)...)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read ticker universe: %w", err)
		}
	}
	if len(rawTickers) == 0 {
		return nil, errors.New("provide at least one ticker or a non-empty --universe FILE")
	}
	seen := make(map[string]struct{}, len(rawTickers))
	tickers := make([]string, 0, len(rawTickers))
	for _, raw := range rawTickers {
		ticker, err := CommandTicker([]string{raw})
		if err != nil {
			return nil, err
		}
		if _, ok := seen[ticker]; ok {
			continue
		}
		seen[ticker] = struct{}{}
		tickers = append(tickers, ticker)
	}
	return tickers, nil
}

func CommandTicker(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("provide exactly one ticker symbol, for example AAPL")
	}
	ticker := strings.ToUpper(strings.TrimSpace(args[0]))
	if !tickerPattern.MatchString(ticker) || ticker == "." || ticker == ".." {
		return "", fmt.Errorf("invalid ticker %q", args[0])
	}
	return ticker, nil
}

// ParseCommandFlags accepts the documented `command TICKER --option value`
// order while retaining the standard library flag parser and its validation.
func ParseCommandFlags(flags *flag.FlagSet, args []string, valueFlags ...string) error {
	valueFlagSet := make(map[string]bool, len(valueFlags))
	for _, name := range valueFlags {
		valueFlagSet[name] = true
	}
	var options, positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		options = append(options, arg)
		name := strings.TrimLeft(arg, "-")
		if before, _, found := strings.Cut(name, "="); found {
			name = before
		}
		if valueFlagSet[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
			options = append(options, args[i+1])
			i++
		}
	}
	return flags.Parse(append(options, positionals...))
}

func ValuationInput(values map[string]float64, ticker string) float64 {
	if value, ok := values[strings.ToUpper(strings.TrimSpace(ticker))]; ok {
		return value
	}
	for key, value := range values {
		if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(ticker)) {
			return value
		}
	}
	return 0
}
