package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"linkedin-profile-api/internal/linkedin"
)

// errBusy signals the global fetch cap is full (the handler maps it to 429).
var errBusy = errors.New("server busy — too many fetches in flight")

// supportedTypes is returned in 400s so API consumers discover what works.
var supportedTypes = []string{"/in/", "/company/", "/school/"}

// handleProfile is the one real endpoint: classify the incoming LinkedIn
// URL, dispatch to the right engine call, return the JSON.
//
//	GET /v1/profile?url=https://www.linkedin.com/in/<name>/
//	GET /v1/profile?url=https://www.linkedin.com/company/<slug>/
//	GET /v1/profile?url=https://www.linkedin.com/school/<slug>/
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing required query parameter: url")
		return
	}

	typ, slug, err := linkedin.ClassifyURL(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     err.Error(),
			"supported": supportedTypes,
		})
		return
	}

	// Cache first: repeat requests for the same URL never touch LinkedIn.
	key := typ.String() + "/" + slug
	if data, ok := s.cache.get(key); ok {
		w.Header().Set("X-Cache", "hit")
		writeJSONBytes(w, http.StatusOK, data)
		return
	}

	// Singleflight: concurrent requests for the same URL share ONE upstream
	// fetch — the owner works, waiters block and get the same bytes.
	servedStale := false
	data, err := s.flights.do(key, func() ([]byte, error) {
		// a waiter may have queued while the owner fetched → cache is hot now
		if data, ok := s.cache.get(key); ok {
			return data, nil
		}

		// Global fetch cap: too many live fetches → fail fast, don't queue.
		select {
		case s.fetchSem <- struct{}{}:
			defer func() { <-s.fetchSem }()
		default:
			return nil, errBusy
		}

		var result any
		var fetchErr error
		switch typ {
		case linkedin.URLTypeProfile:
			result, fetchErr = s.client.FetchProfile(slug, "https://www.linkedin.com/in/"+slug+"/")
		case linkedin.URLTypeCompany, linkedin.URLTypeSchool:
			result, fetchErr = s.client.GetCompany(slug, typ.String())
		}
		if fetchErr != nil {
			// Stale-if-error: serve the last good response (any age) rather
			// than erroring — a session expiry or account restriction then
			// degrades the demo to cached data instead of 502s.
			if stale, ok := s.cache.getStale(key); ok {
				servedStale = true
				return stale, nil
			}
			return nil, fetchErr
		}

		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(result); err != nil {
			return nil, err
		}
		data := buf.Bytes()
		s.cache.set(key, data)
		return data, nil
	})
	if errors.Is(err, errBusy) {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusTooManyRequests, "server busy — too many fetches in flight, retry shortly")
		return
	}
	if err != nil {
		// Upstream failure: map to CLEAN client-facing errors — the real
		// error (internal URLs, voyager details) is logged, never leaked.
		log.Printf("linkedin fetch failed: %v", err)
		switch {
		case errors.Is(err, linkedin.ErrSessionExpired):
			writeError(w, http.StatusUnauthorized, "linkedin session expired — re-run scripts/linkedin_login.py")
		case errors.Is(err, linkedin.ErrProfileNotFound):
			writeError(w, http.StatusNotFound, "profile not found (or not visible to this account)")
		default:
			writeError(w, http.StatusBadGateway, "linkedin upstream error — retry shortly")
		}
		return
	}
	if servedStale {
		w.Header().Set("X-Cache", "stale")
	}
	writeJSONBytes(w, http.StatusOK, data)
}

// handleHealth is a liveness probe for deploys and uptime checks.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON encodes v as JSON. SetEscapeHTML(false) keeps profile URLs and
// image links readable ("&" instead of "&").
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // nothing sensible to do once the response has started
}

// writeJSONBytes writes pre-marshaled JSON (the cache stores response bytes).
func writeJSONBytes(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
}

// writeError is the uniform error shape: {"error": "..."}.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
