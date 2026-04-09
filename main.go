package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type Store struct {
	mu     sync.RWMutex
	byCode map[string]string // code -> original URL
	byURL  map[string]string // original URL -> code
	visits map[string]int    // code -> visit count
}

func NewStore() *Store {
	return &Store{
		byCode: make(map[string]string),
		byURL:  make(map[string]string),
		visits: make(map[string]int),
	}
}

const codeLen = 6
const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func (s *Store) generateCode() string {
	for {
		b := make([]byte, codeLen)
		for i := range b {
			b[i] = charset[rand.Intn(len(charset))]
		}
		code := string(b)
		if _, exists := s.byCode[code]; !exists {
			return code
		}
	}
}

// Shorten возвращает короткий код для URL. Идемпотентен.
func (s *Store) Shorten(rawURL string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if code, exists := s.byURL[rawURL]; exists {
		return code, false
	}

	code := s.generateCode()
	s.byCode[code] = rawURL
	s.byURL[rawURL] = code
	s.visits[code] = 0
	return code, true
}

// Resolve возвращает оригинальный URL и инкрементирует счётчик.
func (s *Store) Resolve(code string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rawURL, exists := s.byCode[code]
	if !exists {
		return "", false
	}
	s.visits[code]++
	return rawURL, true
}

// Stats возвращает URL и количество визитов.
func (s *Store) Stats(code string) (string, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rawURL, exists := s.byCode[code]
	if !exists {
		return "", 0, false
	}
	return rawURL, s.visits[code], true
}

type Server struct {
	store   *Store
	baseURL string
}

func NewServer(store *Store, baseURL string) *Server {
	return &Server{store: store, baseURL: baseURL}
}

type shortenRequest struct {
	URL string `json:"url"`
}

type shortenResponse struct {
	ShortURL string `json:"short_url"`
}

type statsResponse struct {
	URL    string `json:"url"`
	Visits int    `json:"visits"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func isValidURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Host != ""
}

func (s *Server) handleShorten(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	var req shortenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}

	if !isValidURL(req.URL) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid url: scheme and host are required"})
		return
	}

	code, created := s.store.Shorten(req.URL)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, shortenResponse{ShortURL: s.baseURL + "/" + code})
}

func (s *Server) handleRedirect(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/")
	if code == "" {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
		return
	}

	rawURL, ok := s.store.Resolve(code)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "short url not found"})
		return
	}
	http.Redirect(w, r, rawURL, http.StatusMovedPermanently)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/stats/")
	if code == "" {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
		return
	}

	rawURL, visits, ok := s.store.Stats(code)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "short url not found"})
		return
	}
	writeJSON(w, http.StatusOK, statsResponse{URL: rawURL, Visits: visits})
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/shorten", s.handleShorten)
	mux.HandleFunc("/stats/", s.handleStats)
	mux.HandleFunc("/", s.handleRedirect)
	return mux
}

func main() {
	store := NewStore()
	srv := NewServer(store, "http://localhost:8080")

	log.Println("Starting URL Shortener on :8080")
	log.Fatal(http.ListenAndServe(":8080", srv.Routes()))
}
