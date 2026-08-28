package server

import (
	"encoding/json"
	"net/http"

	"linkedin-profile-api/internal/linkedin"
)

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

	var result any
	switch typ {
	case linkedin.URLTypeProfile:
		result, err = s.client.FetchProfile(slug, "https://www.linkedin.com/in/"+slug+"/")
	case linkedin.URLTypeCompany, linkedin.URLTypeSchool:
		result, err = s.client.GetCompany(slug, typ.String())
	default: // ClassifyURL already rejects unsupported types; belt-and-braces
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "unsupported LinkedIn URL type",
			"supported": supportedTypes,
		})
		return
	}
	if err != nil {
		// Upstream failure: session rejected, rate limited, not found, network.
		writeError(w, http.StatusBadGateway, "linkedin fetch failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
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

// writeError is the uniform error shape: {"error": "..."}.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
