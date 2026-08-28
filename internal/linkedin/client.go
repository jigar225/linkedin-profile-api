// Package linkedin is our reverse-engineered client for LinkedIn's
// internal endpoints (Voyager GraphQL + flagship-web RSC actions).
// No browser — plain HTTP, authenticated with our own session cookies.
package linkedin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// userAgent mirrors a desktop Chrome browser. LinkedIn checks it loosely;
// the captured real Voyager calls used a similar string.
const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// Client talks to LinkedIn's internal endpoints using session cookies.
type Client struct {
	http       *http.Client
	cookieHdr  string // prebuilt "k1=v1; k2=v2" header
	csrfToken  string // JSESSIONID value, quotes stripped — LinkedIn's csrf-token header
}

// sessionFile mirrors Playwright's storage_state JSON (linkedin_session.json).
type sessionFile struct {
	Cookies []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"cookies"`
}

// NewClientFromSessionFile builds a Client from a Playwright storage_state file.
func NewClientFromSessionFile(path string) (*Client, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session file: %w", err)
	}
	var sf sessionFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		return nil, fmt.Errorf("parse session file: %w", err)
	}

	var parts []string
	csrf := ""
	for _, c := range sf.Cookies {
		parts = append(parts, c.Name+"="+c.Value)
		if c.Name == "JSESSIONID" {
			csrf = strings.Trim(c.Value, `"`)
		}
	}
	if csrf == "" {
		return nil, fmt.Errorf("session file has no JSESSIONID cookie")
	}

	return &Client{
		http:      &http.Client{Timeout: 30 * time.Second},
		cookieHdr: strings.Join(parts, "; "),
		csrfToken: csrf,
	}, nil
}

// newRequest builds an authenticated request with the base headers every
// LinkedIn internal endpoint expects (verified from captured real calls).
// body may be nil for GETs.
func (c *Client) newRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("cookie", c.cookieHdr)
	req.Header.Set("user-agent", userAgent)
	req.Header.Set("csrf-token", c.csrfToken)
	req.Header.Set("x-li-lang", "en_US")
	req.Header.Set("accept-language", "en-US,en;q=0.9")
	return req, nil
}
