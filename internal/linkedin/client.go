// Package linkedin is our reverse-engineered client for LinkedIn's
// internal endpoints (Voyager GraphQL + flagship-web RSC actions).
// No browser — plain HTTP, authenticated with our own session cookies.
package linkedin

import (
	crand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// ErrSessionExpired signals LinkedIn killed our session server-side
// (dead li_at). Detection: the 302-to-self loop (with li_at deletion) or a
// bounce to the login/authwall/checkpoint pages. Definitive — never retried.
var ErrSessionExpired = errors.New("linkedin session expired — refresh LI_AT/JSESSIONID cookies")

// chromeVersion pins what our HEADERS claim. It must match the account's
// device dossier (the real browser the session was born in — captured:
// Chrome 151), NOT the TLS profile: adjacent Chrome majors share JA3/JA4,
// but a header-vs-dossier version mismatch is replay evidence.
const chromeVersion = "151"

// userAgent mirrors a desktop Chrome browser. LinkedIn checks it loosely;
// the captured real Voyager calls used a similar string.
const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + chromeVersion + ".0.0.0 Safari/537.36"

// secChUA mirrors the client-hint header real Chrome sends (captured shape:
// "Chromium";v="<major>", "Not=A?Brand";v="99").
const secChUA = `"Chromium";v="` + chromeVersion + `", "Not=A?Brand";v="99"`

// Client talks to LinkedIn's internal endpoints using session cookies.
type Client struct {
	http      tls_client.HttpClient
	cookieHdr string // prebuilt "k1=v1; k2=v2" header
	csrfToken string // JSESSIONID value, quotes stripped — LinkedIn's csrf-token header

	// page carries the per-profile-view tracking IDs (nil outside profile
	// fetches). The SPA generates these FRESH on every page load — replaying
	// frozen values across thousands of profiles is impossible traffic.
	page *pageContext
}

// pageContext is one profile-view's set of organic tracking IDs, generated
// fresh per fetch (captured shapes, network_log.jsonl census):
//
//	x-li-page-instance:        urn:li:page:<pageKey>;<pageInstanceID>
//	x-li-pageforestid:         <8 hex boot prefix><24 random hex>  (trace id)
//	x-li-traceparent:          00-<pageForestID>-<spanID>-00       (W3C shape)
//	x-li-tracestate:           LinkedIn=<spanID>
//	x-li-application-instance: <16 bytes base64>                   (per boot)
type pageContext struct {
	pageInstanceID string // 16 random bytes, base64
	pageForestID   string // boot prefix + 24 random hex
	appInstance    string // 16 random bytes, base64
}

// appBoot mimics the browser boot: all pageForestIDs from one boot share an
// 8-hex prefix (captured boots: 00065a19, 00065a1a — the family shape is
// "0006xxxx"), and the application-instance is stable for the process.
var appBoot = struct {
	prefix   string
	instance string
}{
	prefix:   "0006" + randomHex(4),
	instance: randomB64(16),
}

// newPageContext mints one profile-view's IDs. Prefix semantics are opaque
// (server-issued?) — we reproduce the observed shape and keep every derived
// header internally consistent, which is what the wire can check.
func newPageContext() *pageContext {
	return &pageContext{
		pageInstanceID: randomB64(16),
		pageForestID:   appBoot.prefix + randomHex(24),
		appInstance:    appBoot.instance,
	}
}

// randomHex returns n random lowercase-hex characters.
func randomHex(n int) string {
	b := make([]byte, (n+1)/2)
	if _, err := crand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)[:n]
}

// randomB64 returns n random bytes, standard base64 (with +/= padding —
// exactly how the captured IDs look).
func randomB64(n int) string {
	b := make([]byte, n)
	if _, err := crand.Read(b); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// forProfile returns a shallow copy carrying a FRESH page context — one per
// profile view, mirroring the SPA. The HTTP client (connection pool) and
// auth are shared; concurrent fetches each get their own IDs.
func (c *Client) forProfile() *Client {
	return &Client{
		http:      c.http,
		cookieHdr: c.cookieHdr,
		csrfToken: c.csrfToken,
		page:      newPageContext(),
	}
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
		// The lang cookie pins the locale: without it, flagship-web serves the
		// geo-IP locale and IGNORES x-li-lang (verified on Railway: a NL-geo'd
		// egress IP got Dutch — "Op locatie" instead of "On-site" — which
		// silently kills every English-anchored parser).
		cookieHdr: `li_at=` + strings.TrimSpace(liAt) + `; JSESSIONID="` + csrf + `"; lang=v=2&lang=en-us`,
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
	return NewClientFromSessionJSON(raw)
}

// NewClientFromSessionJSON builds a Client from storage_state BYTES — for
// deploys where the jar rides an env var (LINKEDIN_SESSION_JSON) instead of
// a file (Railway et al. have no handy filesystem for secrets).
func NewClientFromSessionJSON(raw []byte) (*Client, error) {
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

// newHTTPClient builds the shared HTTP client on a Chrome-impersonating TLS
// stack. LinkedIn (Akamai-fronted) fingerprints the TRANSPORT before reading
// a single header: Go's stock net/http sends a world-famous ClientHello —
// different ciphers, extensions and curves than ANY browser — while our
// headers claim "Chrome on macOS". That mismatch is a session killer. The
// Chrome profile makes the ClientHello, HTTP/2 SETTINGS and pseudo-header
// order byte-identical to real Chrome; RandomTLSExtensionOrder reproduces
// Chrome's per-connection extension shuffling (since 2023 a static
// ClientHello is itself a beacon — real Chrome never sends the same JA3
// twice). No cookie jar: we set the Cookie header manually per request.
func newHTTPClient() tls_client.HttpClient {
	// Session-death detection, same rules as the old CheckRedirect:
	// LinkedIn bounces dead sessions to the login/authwall/checkpoint pages,
	// or 302s to the SAME URL in a loop (with li_at deletion). Fail fast
	// with a TYPED error — retrying a dead session just hammers the risk
	// engine.
	redirectFunc := func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && req.URL.String() == via[len(via)-1].URL.String() {
			return ErrSessionExpired // 302-to-self loop
		}
		if strings.Contains(req.URL.Path, "/uas/") ||
			strings.Contains(req.URL.Path, "/authwall") ||
			strings.Contains(req.URL.Path, "/checkpoint/") {
			return ErrSessionExpired
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		log.Printf("linkedin: redirect #%d → %s", len(via), req.URL)
		return nil
	}

	opts := []tls_client.HttpClientOption{
		tls_client.WithClientProfile(profiles.Chrome_146),
		tls_client.WithRandomTLSExtensionOrder(),
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithCustomRedirectFunc(redirectFunc),
	}
	// LINKEDIN_PROXY: route everything through a residential proxy. REQUIRED
	// for cloud deploys — a session born on a residential IP but used from a
	// datacenter egress is the community-proven #1 kill pattern ("impossible
	// travel"). Match the proxy's geography to the birth machine's.
	if proxy := os.Getenv("LINKEDIN_PROXY"); proxy != "" {
		opts = append(opts, tls_client.WithProxyUrl(proxy))
		log.Printf("linkedin: routing via proxy %s", proxyHostOnly(proxy))
	}

	idleTimeout := 90 * time.Second
	// Pool covers concurrent profile fetches without fresh TLS handshakes
	// per request (and every handshake here is the expensive impersonated
	// kind).
	opts = append(opts, tls_client.WithTransportOptions(&tls_client.TransportOptions{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		MaxConnsPerHost:     10,
		IdleConnTimeout:     &idleTimeout,
	}))
	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), opts...)
	if err != nil {
		// Only misconfiguration can fail here — a programmer error, loud.
		panic(fmt.Sprintf("tls client build: %v", err))
	}
	return client
}

// proxyHostOnly extracts the host for logging — proxy URLs carry credentials
// that must never hit the logs.
func proxyHostOnly(rawurl string) string {
	if i := strings.Index(rawurl, "://"); i >= 0 {
		rawurl = rawurl[i+3:]
	}
	if i := strings.LastIndex(rawurl, "@"); i >= 0 {
		rawurl = rawurl[i+1:]
	}
	return rawurl
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
		if errors.Is(err, ErrSessionExpired) {
			return nil, err // session death is definitive — never retried
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
	// Client hints + fetch metadata every real Chrome XHR carries. Absence
	// of the whole sec-* family is a headless-client tell (layer 3).
	req.Header.Set("sec-ch-ua", secChUA)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	// Organic tracking-ID family (captured on every real profile-view call).
	// Fresh per profile fetch via forProfile; span unique per request.
	if c.page != nil {
		span := randomHex(16)
		req.Header.Set("x-li-pageforestid", c.page.pageForestID)
		req.Header.Set("x-li-traceparent", "00-"+c.page.pageForestID+"-"+span+"-00")
		req.Header.Set("x-li-tracestate", "LinkedIn="+span)
		req.Header.Set("x-li-application-instance", c.page.appInstance)
		req.Header.Set("x-li-page-instance", "urn:li:page:d_flagship3_profile_view_base;"+c.page.pageInstanceID)
		req.Header.Set("x-li-page-instance-tracking-id", c.page.pageInstanceID)
	}
	return req, nil
}
