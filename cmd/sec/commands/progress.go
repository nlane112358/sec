package commands

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner renders a single-line inline step progress indicator when network hydration occurs.
type Spinner struct {
	mu      sync.Mutex
	w       io.Writer
	isTTY   bool
	start   time.Time
	steps   []string
	frame   int
	active  bool
	stopped bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

func NewSpinner(w io.Writer) *Spinner {
	return &Spinner{
		w:     w,
		isTTY: isTerminalWriter(w),
		start: time.Now(),
	}
}

func (s *Spinner) Step(msg string) {
	if s == nil || s.w == nil || strings.TrimSpace(msg) == "" {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if len(s.steps) == 0 || s.steps[len(s.steps)-1] != msg {
		s.steps = append(s.steps, msg)
	}
	if s.isTTY && !s.active {
		s.active = true
		s.stopCh = make(chan struct{})
		s.doneCh = make(chan struct{})
		s.renderLocked()
		go s.loop()
	} else if s.isTTY {
		s.renderLocked()
	}
	s.mu.Unlock()
}

// ReplaceFirstStep updates the initial step message (e.g. once form family is detected).
func (s *Spinner) ReplaceFirstStep(msg string) {
	if s == nil || s.w == nil || strings.TrimSpace(msg) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	if len(s.steps) == 0 {
		s.steps = append(s.steps, msg)
	} else {
		s.steps[0] = msg
	}
	if s.isTTY && s.active {
		s.renderLocked()
	}
}

func (s *Spinner) loop() {
	ticker := time.NewTicker(90 * time.Millisecond)
	defer ticker.Stop()
	defer close(s.doneCh)
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			if !s.stopped {
				s.frame = (s.frame + 1) % len(spinnerFrames)
				s.renderLocked()
			}
			s.mu.Unlock()
		}
	}
}

func (s *Spinner) renderLocked() {
	if s.w == nil || len(s.steps) == 0 {
		return
	}
	line := strings.Join(s.steps, " ")
	_, _ = fmt.Fprintf(s.w, "\r\033[K%s %s", spinnerFrames[s.frame], line)
}

// Finish stops the spinner and writes the completed step summary if any steps occurred.
func (s *Spinner) Finish(err error) {
	if s == nil || s.w == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	active := s.active
	stopCh := s.stopCh
	doneCh := s.doneCh
	hasSteps := len(s.steps) > 0
	line := strings.Join(s.steps, " ")
	elapsed := time.Since(s.start)
	s.mu.Unlock()

	if active {
		close(stopCh)
		<-doneCh
	}

	if !hasSteps {
		return
	}
	prefix := ""
	if s.isTTY {
		prefix = "\r\033[K"
	}
	if err != nil {
		_, _ = fmt.Fprintf(s.w, "%s%s Failed (%s)\n", prefix, line, formatElapsedBrief(elapsed))
		return
	}
	_, _ = fmt.Fprintf(s.w, "%s%s Done in %s\n", prefix, line, formatElapsedBrief(elapsed))
}

func formatElapsedBrief(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	mins := int(d / time.Minute)
	secs := int((d % time.Minute) / time.Second)
	if mins >= 60 {
		hrs := mins / 60
		mins = mins % 60
		return fmt.Sprintf("%dh%02dm", hrs, mins)
	}
	return fmt.Sprintf("%dm%02ds", mins, secs)
}

// BatchProgressBar displays a clean inline status bar with cache hits vs fetches and ETA.
type BatchProgressBar struct {
	mu              sync.Mutex
	w               io.Writer
	isTTY           bool
	label           string
	total           int
	cacheHits       int
	pending         int
	ratePerSec      float64
	start           time.Time
	lastRender      time.Time
	lastPctPrinted  int
	finished        bool
}

func NewBatchProgressBar(w io.Writer, label string, total, cacheHits, pending int, expectedItemsPerSec float64) *BatchProgressBar {
	if expectedItemsPerSec <= 0 {
		expectedItemsPerSec = 4.25
	}
	b := &BatchProgressBar{
		w:              w,
		isTTY:          isTerminalWriter(w),
		label:          label,
		total:          total,
		cacheHits:      cacheHits,
		pending:        pending,
		ratePerSec:     expectedItemsPerSec,
		start:          time.Now(),
		lastPctPrinted: -1,
	}
	if w != nil && pending > 0 {
		b.Update(0, 0, "")
	}
	return b
}

func (b *BatchProgressBar) Update(fetched, failed int, currentTicker string) {
	if b == nil || b.w == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finished {
		return
	}
	now := time.Now()
	doneFetches := fetched + failed
	processed := b.cacheHits + doneFetches
	if processed > b.total {
		processed = b.total
	}
	pct := 100
	if b.total > 0 {
		pct = processed * 100 / b.total
	}

	if b.isTTY {
		if !b.lastRender.IsZero() && now.Sub(b.lastRender) < 85*time.Millisecond && doneFetches < b.pending {
			return
		}
		b.lastRender = now
		bar := renderProgressBar(processed, b.total, 20)
		eta := b.estimateETA(doneFetches, now)
		suffix := ""
		if currentTicker != "" {
			suffix = " · " + currentTicker
		}
		_, _ = fmt.Fprintf(b.w, "\r\033[K%s [%s] %d/%d (%d%%) · cache: %d · fetched: %d · failed: %d · ETA %s%s",
			b.label, bar, processed, b.total, pct, b.cacheHits, fetched, failed, eta, suffix)
		return
	}

	// Non-TTY: emit concise milestone updates (start + every 25%) without log flooding.
	milestone := (pct / 25) * 25
	if b.lastPctPrinted < 0 || (milestone > b.lastPctPrinted && doneFetches > 0) {
		b.lastPctPrinted = milestone
		eta := b.estimateETA(doneFetches, now)
		_, _ = fmt.Fprintf(b.w, "%s: %d/%d (%d%%) · cache: %d · fetched: %d · failed: %d · ETA %s\n",
			b.label, processed, b.total, pct, b.cacheHits, fetched, failed, eta)
	}
}

func (b *BatchProgressBar) estimateETA(doneFetches int, now time.Time) string {
	remaining := b.pending - doneFetches
	if remaining <= 0 {
		return "0s"
	}
	rate := b.ratePerSec
	elapsed := now.Sub(b.start).Seconds()
	if elapsed >= 1.5 && doneFetches >= 2 {
		observed := float64(doneFetches) / elapsed
		if observed > 0 {
			rate = observed
		}
	}
	etaDur := time.Duration(float64(remaining)/rate*float64(time.Second)) + 500*time.Millisecond
	return formatElapsedBrief(etaDur)
}

func (b *BatchProgressBar) Finish(fetched, failed int) {
	if b == nil || b.w == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finished {
		return
	}
	b.finished = true
	elapsed := time.Since(b.start)
	processed := b.cacheHits + fetched + failed
	if processed > b.total {
		processed = b.total
	}
	prefix := ""
	if b.isTTY {
		prefix = "\r\033[K"
	}
	bar := renderProgressBar(processed, b.total, 20)
	_, _ = fmt.Fprintf(b.w, "%s%s [%s] %d/%d · cache: %d · fetched: %d · failed: %d · Done in %s\n",
		prefix, b.label, bar, processed, b.total, b.cacheHits, fetched, failed, formatElapsedBrief(elapsed))
}

func renderProgressBar(current, total, width int) string {
	if total <= 0 || width <= 0 {
		return strings.Repeat("█", width)
	}
	filled := (current * width) / total
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
