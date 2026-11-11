package edgar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const secBase = "https://data.sec.gov"
const tickerURL = "https://www.sec.gov/files/company_tickers.json"
const secRequestRate = 8.5
const tickerCacheTTL = 24 * time.Hour

var secRequestLimiter = newTokenBucket(secRequestRate, 1)
var tickerMappingMu sync.Mutex

type tokenBucket struct {
	mu       sync.Mutex
	rate     float64
	capacity float64
	tokens   float64
	updated  time.Time
}

func newTokenBucket(rate, capacity float64) *tokenBucket {
	return &tokenBucket{
		rate:     rate,
		capacity: capacity,
		tokens:   capacity,
	}
}

func (b *tokenBucket) Wait(ctx context.Context) error {
	b.mu.Lock()
	now := time.Now()
	if b.updated.IsZero() {
		b.updated = now
	} else {
		elapsed := now.Sub(b.updated).Seconds()
		b.tokens += elapsed * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.updated = now
	}
	b.tokens--
	var delay time.Duration
	if b.tokens < 0 {
		delay = time.Duration(math.Ceil((-b.tokens / b.rate) * float64(time.Second)))
	}
	b.mu.Unlock()

	return waitContext(ctx, delay)
}

type Client struct {
	http      *http.Client
	userAgent string
}

type tickerEntry struct {
	CIK    int    `json:"cik_str"`
	Ticker string `json:"ticker"`
}

type tickerMapCache struct {
	FetchedAt time.Time              `json:"fetched_at"`
	Entries   map[string]tickerEntry `json:"entries"`
}

func NewClient(userAgent string) *Client {
	return &Client{
		http: &http.Client{
			Timeout: 20 * time.Second,
		},
		userAgent: userAgent,
	}
}

func (c *Client) get(
	ctx context.Context,
	url string,
	dst any,
) error {
	var lastErr error
	var retryDelay time.Duration

	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			if err := waitContext(ctx, retryDelay); err != nil {
				return err
			}
		}
		if err := secRequestLimiter.Wait(ctx); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(
			ctx, http.MethodGet, url, nil,
		)
		if err != nil {
			return err
		}

		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			retryDelay = retryBackoff(attempt)
			continue
		}

		body, readErr := io.ReadAll(
			io.LimitReader(resp.Body, 64<<20),
		)
		resp.Body.Close()

		if readErr != nil {
			lastErr = readErr
			retryDelay = retryBackoff(attempt)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode >= 500 {
			lastErr = fmt.Errorf(
				"SEC returned HTTP %d", resp.StatusCode,
			)
			retryDelay = retryBackoff(attempt)
			if resp.StatusCode == http.StatusTooManyRequests {
				if serverDelay, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok && serverDelay > retryDelay {
					retryDelay = serverDelay
				}
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf(
				"SEC returned HTTP %d: %s",
				resp.StatusCode, strings.TrimSpace(string(body)),
			)
		}

		if err := json.Unmarshal(body, dst); err != nil {
			return fmt.Errorf("decode SEC response: %w", err)
		}

		return nil
	}

	return fmt.Errorf("request failed after retries: %w", lastErr)
}

func retryBackoff(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt)) * 500 * time.Millisecond
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		maxSeconds := int64((time.Duration(1<<63 - 1)) / time.Second)
		if seconds > maxSeconds {
			return time.Duration(1<<63 - 1), true
		}
		return time.Duration(seconds) * time.Second, true
	}
	date, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if !date.After(now) {
		return 0, true
	}
	return date.Sub(now), true
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) ResolveTicker(ctx context.Context, ticker string) (string, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if ticker == "" {
		return "", fmt.Errorf("ticker cannot be empty")
	}
	entries, err := c.companyTickerMapping(ctx)
	if err != nil {
		return "", fmt.Errorf("look up ticker %q: %w", ticker, err)
	}
	var match string
	for _, entry := range entries {
		if strings.EqualFold(entry.Ticker, ticker) {
			if match != "" && match != strconv.Itoa(entry.CIK) {
				return "", fmt.Errorf("ticker %q matches multiple companies", ticker)
			}
			match = strconv.Itoa(entry.CIK)
		}
	}
	if match == "" {
		return "", fmt.Errorf("ticker %q not found in SEC company list", ticker)
	}
	return NormaliseCIK(match)
}

func (c *Client) companyTickerMapping(ctx context.Context) (map[string]tickerEntry, error) {
	tickerMappingMu.Lock()
	defer tickerMappingMu.Unlock()

	cachePath := tickerCachePath()
	var stale *tickerMapCache
	if data, err := os.ReadFile(cachePath); err == nil {
		var cached tickerMapCache
		if json.Unmarshal(data, &cached) == nil && len(cached.Entries) > 0 {
			age := time.Since(cached.FetchedAt)
			if !cached.FetchedAt.IsZero() && age >= 0 && age < tickerCacheTTL {
				return cached.Entries, nil
			}
			stale = &cached
		}
	}

	var entries map[string]tickerEntry
	if err := c.get(ctx, tickerURL, &entries); err != nil {
		if stale != nil {
			return stale.Entries, nil
		}
		return nil, err
	}
	if len(entries) > 0 {
		_ = writeTickerCache(cachePath, tickerMapCache{
			FetchedAt: time.Now().UTC(),
			Entries:   entries,
		})
	}
	return entries, nil
}

func tickerCachePath() string {
	if dir := strings.TrimSpace(os.Getenv("SEC_CACHE_DIR")); dir != "" {
		return filepath.Join(dir, "company_tickers.json")
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = strings.TrimSpace(os.Getenv("SEC_DATABASE_DIR"))
		if dir == "" {
			dir = "data"
		}
		dir = filepath.Join(dir, ".cache")
	}
	return filepath.Join(dir, "sec", "company_tickers.json")
}

func writeTickerCache(path string, cache tickerMapCache) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".company_tickers-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := json.NewEncoder(temp).Encode(cache); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func (c *Client) Facts(
	ctx context.Context, cik string,
) (*CompanyFacts, error) {
	var result CompanyFacts
	url := secBase + "/api/xbrl/companyfacts/CIK" +
		cik + ".json"

	if err := c.get(ctx, url, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Submissions(ctx context.Context, cik string) (*CompanySubmissions, error) {
	var result CompanySubmissions
	url := secBase + "/submissions/CIK" + cik + ".json"
	if err := c.get(ctx, url, &result); err != nil {
		return nil, err
	}
	for _, file := range result.Filings.Files {
		if !validSupplementalName(file.Name) {
			return nil, fmt.Errorf("SEC returned invalid supplementary submissions filename %q", file.Name)
		}
		var history FilingHistory
		if err := c.get(ctx, secBase+"/submissions/"+file.Name, &history); err != nil {
			return nil, fmt.Errorf("fetch supplementary submissions %s: %w", file.Name, err)
		}
		result.Filings.Supplemental = append(result.Filings.Supplemental, history)
	}
	return &result, nil
}

func validSupplementalName(name string) bool {
	if !strings.HasPrefix(name, "CIK") || !strings.HasSuffix(name, ".json") || strings.Contains(name, "..") || strings.ContainsAny(name, `/\\?#`) {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_' {
			continue
		}
		return false
	}
	return true
}
