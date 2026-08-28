// Package linkedin is our reverse-engineered client for LinkedIn's
// internal endpoints (Voyager GraphQL + flagship-web RSC actions).
// No browser — plain HTTP, authenticated with our own session cookies.
package linkedin

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
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
	http      *http.Client
	cookieHdr string // prebuilt "k1=v1; k2=v2" header
	csrfToken string // JSESSIONID value, quotes stripped — LinkedIn's csrf-token header
}

// NewClient builds a Client from the two cookies that actually matter:
// li_at (auth) + JSESSIONID (CSRF). Values may be pasted from DevTools
// with or without surrounding quotes — we normalize either way.
func NewClient(liAt, jsessionID string) *Client {
	csrf := strings.Trim(strings.TrimSpace(jsessionID), `"`)
	return &Client{
		http: newHTTPClient(),
		// LinkedIn sets JSESSIONID as a quoted cookie value ("ajax:...") —
		// reproduce that in the Cookie header; the csrf-token header wants it raw.
		cookieHdr: `li_at=` + strings.TrimSpace(liAt) + `; JSESSIONID="` + csrf + `"`,
		csrfToken: csrf,
	}
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
		http:      newHTTPClient(),
		cookieHdr: strings.Join(parts, "; "),
		csrfToken: csrf,
	}, nil
}

// newHTTPClient builds the shared HTTP client, tuned for our burst pattern:
// a profile fetch fires ~11 requests at one host — the pool must cover the
// burst or every request opens a fresh TLS handshake (default transport pools
// only 2/host; handshake storms were observed as intermittent
// "tls: bad record MAC" failures).
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        20,
			MaxIdleConnsPerHost: 10,
			MaxConnsPerHost:     10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
}

// doWithRetry runs req with up to 3 attempts, backing off with jitter between
// tries. Only transport-level errors are retried — a received response (even
// a 500) is a definitive answer. All our calls are read-only: retry is safe.
func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := 300 * time.Millisecond * time.Duration(1<<attempt)
			time.Sleep(time.Duration(rand.Int64N(int64(backoff))))
			// rewind the body for the next attempt (bytes.Reader ⇒ GetBody set)
			if req.GetBody != nil {
				if body, err := req.GetBody(); err == nil {
					req.Body = body
				}
			}
		}
		resp, err := c.http.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
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
