package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPanicRecoveryMiddleware(t *testing.T) {
	panickingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("simulated critical crash")
	})

	handler := recoveryMiddleware(panickingHandler)

	req := httptest.NewRequest("GET", "/api/test-crash", nil)
	rec := httptest.NewRecorder()

	// Should not crash the test process
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on panic, got %d", rec.Code)
	}

	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse json error: %v", err)
	}
	if res["error"] != "internal server error" {
		t.Fatalf("unexpected error message: %v", res)
	}
}

func TestGzipCompressionMiddleware(t *testing.T) {
	payload := `{"status":"ok","message":"` + string(bytes.Repeat([]byte("institutional SEC intelligence "), 50)) + `"}`
	targetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(payload))
	})

	handler := gzipMiddleware(targetHandler)

	// Case 1: Client does not support gzip -> raw response
	t.Run("without gzip accept", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Content-Encoding") == "gzip" {
			t.Fatalf("unexpected gzip encoding")
		}
		if rec.Body.String() != payload {
			t.Fatalf("body mismatch")
		}
	})

	// Case 2: Client accepts gzip -> compressed response
	t.Run("with gzip accept", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("expected Content-Encoding: gzip, got %q", rec.Header().Get("Content-Encoding"))
		}
		if rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatalf("expected Vary: Accept-Encoding, got %q", rec.Header().Get("Vary"))
		}

		// Raw body length should be significantly smaller than payload
		compressedBytes := rec.Body.Bytes()
		if len(compressedBytes) >= len(payload) {
			t.Fatalf("gzip did not reduce payload size: raw=%d, compressed=%d", len(payload), len(compressedBytes))
		}

		// Verify decompression yields original payload
		gr, err := gzip.NewReader(bytes.NewReader(compressedBytes))
		if err != nil {
			t.Fatalf("failed to create gzip reader: %v", err)
		}
		defer gr.Close()

		decompressed, err := io.ReadAll(gr)
		if err != nil {
			t.Fatalf("failed to read decompressed stream: %v", err)
		}
		if string(decompressed) != payload {
			t.Fatalf("decompressed payload mismatch")
		}
	})
}

func TestETagAndConditionalCaching(t *testing.T) {
	data := `{"company":"Apple Inc.","ticker":"AAPL","roic":0.584}`
	targetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(data))
	})

	handler := etagMiddleware(targetHandler)

	// Step 1: Initial GET should return 200 with ETag header
	req1 := httptest.NewRequest("GET", "/api/company/AAPL/summary", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec1.Code)
	}

	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected non-empty ETag header")
	}

	// Step 2: Second GET with If-None-Match should return 304 Not Modified with empty body
	req2 := httptest.NewRequest("GET", "/api/company/AAPL/summary", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified, got %d", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Fatalf("expected empty body for 304, got %d bytes", rec2.Body.Len())
	}
}

func TestCacheControlPolicies(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := cacheControlMiddleware(dummyHandler)

	tests := []struct {
		path            string
		expectedPattern string
	}{
		{"/assets/index-Dk39.js", "max-age=31536000"},
		{"/assets/style-84fa.css", "immutable"},
		{"/", "no-cache"},
		{"/index.html", "no-cache"},
		{"/api/stats", "no-cache"},
		{"/api/company/AAPL/summary", "stale-while-revalidate"},
		{"/api/company/AAPL/quote", "max-age=60"},
		{"/api/health", "no-store"},
	}

	for _, tc := range tests {
		req := httptest.NewRequest("GET", tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		cc := rec.Header().Get("Cache-Control")
		if !bytes.Contains([]byte(cc), []byte(tc.expectedPattern)) {
			t.Errorf("path %s: expected Cache-Control containing %q, got %q", tc.path, tc.expectedPattern, cc)
		}
	}
}

func TestHealthEndpoint(t *testing.T) {
	srv, err := NewServer("../../data/sec.db", "", nil)
	if err != nil {
		t.Skip("skipping integration test without local sec.db")
		return
	}
	defer srv.Close()

	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /api/health, got %d", rec.Code)
	}

	var res HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected status 'ok', got %q", res.Status)
	}
	if res.Database != "connected" {
		t.Fatalf("expected database 'connected', got %q", res.Database)
	}
}
