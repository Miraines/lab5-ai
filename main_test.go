package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupServer() (*Server, http.Handler) {
	store := NewStore()
	srv := NewServer(store, "http://localhost:8080")
	return srv, srv.Routes()
}

func TestShortenValidURL(t *testing.T) {
	_, handler := setupServer()

	body := `{"url": "https://example.com/very/long/path"}`
	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	var resp shortenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !strings.HasPrefix(resp.ShortURL, "http://localhost:8080/") {
		t.Fatalf("unexpected short_url: %s", resp.ShortURL)
	}
}

func TestShortenInvalidURL(t *testing.T) {
	_, handler := setupServer()

	body := `{"url": "not-a-valid-url"}`
	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestShortenEmptyBody(t *testing.T) {
	_, handler := setupServer()

	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(""))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestShortenEmptyURL(t *testing.T) {
	_, handler := setupServer()

	body := `{"url": ""}`
	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestShortenIdempotent(t *testing.T) {
	_, handler := setupServer()

	body := `{"url": "https://example.com"}`

	req1 := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	var resp1 shortenResponse
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatalf("failed to decode first response: %v", err)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 on duplicate, got %d", w2.Code)
	}

	var resp2 shortenResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("failed to decode second response: %v", err)
	}

	if resp1.ShortURL != resp2.ShortURL {
		t.Fatalf("expected same short_url, got %s and %s", resp1.ShortURL, resp2.ShortURL)
	}
}

func TestRedirectExistingCode(t *testing.T) {
	_, handler := setupServer()

	body := `{"url": "https://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp shortenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	code := strings.TrimPrefix(resp.ShortURL, "http://localhost:8080/")

	req2 := httptest.NewRequest(http.MethodGet, "/"+code, nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", w2.Code)
	}
	if loc := w2.Header().Get("Location"); loc != "https://example.com" {
		t.Fatalf("expected redirect to https://example.com, got %s", loc)
	}
}

func TestRedirectNotFound(t *testing.T) {
	_, handler := setupServer()

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestStatsAfterVisits(t *testing.T) {
	_, handler := setupServer()

	body := `{"url": "https://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp shortenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	code := strings.TrimPrefix(resp.ShortURL, "http://localhost:8080/")

	// Visit 3 times
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodGet, "/"+code, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/stats/"+code, nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}

	var stats statsResponse
	if err := json.NewDecoder(w2.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode stats response: %v", err)
	}

	if stats.URL != "https://example.com" {
		t.Fatalf("expected url https://example.com, got %s", stats.URL)
	}
	if stats.Visits != 3 {
		t.Fatalf("expected 3 visits, got %d", stats.Visits)
	}
}
