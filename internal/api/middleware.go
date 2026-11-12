package api

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

var gzipPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return w
	},
}

type statusRecorder struct {
	http.ResponseWriter
	status       int
	bytesWritten int64
	wroteHeader  bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
		r.ResponseWriter.WriteHeader(status)
	}
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytesWritten += int64(n)
	return n, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// recoveryMiddleware catches panics in HTTP handlers, logs the stack trace,
// and returns a structured JSON 500 response rather than terminating the process.
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := string(debug.Stack())
				slog.Error("HTTP panic recovered",
					"error", fmt.Sprint(rec),
					"method", r.Method,
					"path", r.URL.Path,
					"stack", stack,
				)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware records execution metrics for each HTTP request,
// capturing status code, latency, payload size, and remote IP.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		clientIP := r.RemoteAddr
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			clientIP = strings.TrimSpace(strings.Split(xff, ",")[0])
		}

		if rec.status >= 500 {
			slog.Error("HTTP request failed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", duration.String(),
				"bytes", rec.bytesWritten,
				"ip", clientIP,
			)
		} else if duration > 500*time.Millisecond {
			slog.Warn("Slow HTTP request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", duration.String(),
				"bytes", rec.bytesWritten,
				"ip", clientIP,
			)
		} else if strings.HasPrefix(r.URL.Path, "/api/") {
			slog.Info("HTTP request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", duration.String(),
				"bytes", rec.bytesWritten,
			)
		}
	})
}

// corsMiddleware sets institutional CORS headers for web, local dev,
// and Excel/Google Sheets integrations.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, If-None-Match, Accept-Encoding")
		w.Header().Set("Access-Control-Expose-Headers", "ETag, Content-Encoding, Cache-Control")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cacheControlMiddleware sets cache headers according to resource mutability:
// immutable long-term caching for hashed bundle assets, no-cache for index.html,
// and short-to-medium caching with stale-while-revalidate for financial APIs.
func cacheControlMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path

		if strings.HasPrefix(p, "/assets/") {
			// Immutable long-term caching for hashed JS/CSS assets
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else if p == "/" || p == "/index.html" {
			// App entry point must always revalidate
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		} else if strings.HasPrefix(p, "/api/") {
			if strings.Contains(p, "/quote") {
				// Live stock quotes refresh periodically
				w.Header().Set("Cache-Control", "public, max-age=60")
			} else if strings.Contains(p, "/health") || strings.Contains(p, "/stats") {
				// System health and database statistics must never be cached so counts update automatically
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			} else {
				// Fundamental SEC data changes only upon periodic filings
				w.Header().Set("Cache-Control", "public, max-age=300, stale-while-revalidate=60")
			}
		}

		next.ServeHTTP(w, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer      *gzip.Writer
	wroteHeader bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	if status != http.StatusNoContent && status != http.StatusNotModified {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.writer.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if w.writer != nil {
		_ = w.writer.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// gzipMiddleware transparently compresses HTTP response bodies when requested by
// the client via Accept-Encoding: gzip, reducing payload sizes by 75-85%.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			r.Header.Get("Upgrade") != "" ||
			r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		gz := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gz)
		gz.Reset(w)
		defer gz.Close()

		gzw := &gzipResponseWriter{
			ResponseWriter: w,
			writer:         gz,
		}

		next.ServeHTTP(gzw, r)
	})
}

type bufferedAPIWriter struct {
	http.ResponseWriter
	buf         bytes.Buffer
	status      int
	wroteHeader bool
}

func (b *bufferedAPIWriter) WriteHeader(status int) {
	if !b.wroteHeader {
		b.status = status
		b.wroteHeader = true
	}
}

func (b *bufferedAPIWriter) Write(p []byte) (int, error) {
	if !b.wroteHeader {
		b.status = http.StatusOK
		b.wroteHeader = true
	}
	return b.buf.Write(p)
}

// etagMiddleware calculates SHA-256 entity tags for API GET responses and returns
// HTTP 304 Not Modified when the client's If-None-Match matches the content hash.
func etagMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}

		bw := &bufferedAPIWriter{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(bw, r)

		if bw.status == http.StatusOK && bw.buf.Len() > 0 {
			sum := sha256.Sum256(bw.buf.Bytes())
			etag := `"` + hex.EncodeToString(sum[:8]) + `"`
			w.Header().Set("ETag", etag)

			if match := r.Header.Get("If-None-Match"); match != "" {
				if match == etag || match == "*" || strings.Contains(match, etag) {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}

		if bw.wroteHeader && bw.status != 0 {
			w.WriteHeader(bw.status)
		}
		_, _ = w.Write(bw.buf.Bytes())
	})
}
