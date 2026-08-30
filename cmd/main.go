// LinkedIn Profile API — reverse-engineered, browserless.
//
// Server mode (default):  go run ./cmd                       → serves /v1/profile?url=...
// CLI mode (debug):       go run ./cmd -url <linkedin-url>   → one-shot fetch, prints JSON
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"

	"linkedin-profile-api/internal/linkedin"
	"linkedin-profile-api/internal/server"
)

// envOr returns the env var value, or fallback when unset/empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// fileExists reports whether path names a readable regular file.
func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func main() {
	// Log platforms (Railway et al.) classify anything on stderr as
	// error-level; our routine logs are info — send them to stdout.
	log.SetOutput(os.Stdout)

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "warning: .env:", err)
	}

	url := flag.String("url", "", "one-shot CLI mode: fetch this LinkedIn URL and print JSON")
	session := flag.String("session",
		envOr("LINKEDIN_SESSION_FILE", "../linkedin_session.json"),
		"path to session cookies JSON (env: LINKEDIN_SESSION_FILE; default assumes repo root, file kept one level up = outside the repo)")
	flag.Parse()

	var client *linkedin.Client
	liAt, jsessionID := os.Getenv("LI_AT"), os.Getenv("JSESSIONID")
	sessionJSON := os.Getenv("LINKEDIN_SESSION_JSON")
	switch {
	case sessionJSON != "":
		// CLOUD path: the full jar rides an env var (no filesystem for
		// secrets on Railway et al.). Paste the storage_state file's
		// contents verbatim.
		var err error
		client, err = linkedin.NewClientFromSessionJSON([]byte(sessionJSON))
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: LINKEDIN_SESSION_JSON:", err)
			os.Exit(1)
		}
		fmt.Println("auth: LINKEDIN_SESSION_JSON env (full jar)")
	case fileExists(*session):
		// LOCAL path: the full cookie jar from a browser-born session (cold
		// path — scripts/linkedin_login.py). Replay consistency needs the
		// WHOLE jar: bcookie is LinkedIn's device ID and the session's
		// dossier expects it (docs/recon.md, round 7).
		var err error
		client, err = linkedin.NewClientFromSessionFile(*session)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Println("auth: session file (full jar):", *session)
	case liAt != "" && jsessionID != "":
		// FALLBACK: stripped jar (li_at + JSESSIONID only) — works, but the
		// missing device cookies weaken the replay story. Prefer the jar.
		client = linkedin.NewClient(liAt, jsessionID)
		fmt.Println("auth: LI_AT + JSESSIONID env cookies (stripped jar — prefer the session jar)")
	case liAt != "" || jsessionID != "":
		fmt.Fprintln(os.Stderr, "error: set BOTH LI_AT and JSESSIONID, or neither (falls back to session jar)")
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "error: no auth found — run scripts/linkedin_login.py to birth a session, or set LINKEDIN_SESSION_JSON / LI_AT + JSESSIONID")
		os.Exit(1)
	}

	if *url != "" {
		runCLI(client, *url)
		return
	}

	port := envOr("PORT", "8080")
	if err := server.New(client).ListenAndServe(":" + port); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}

// runCLI fetches one URL and prints the JSON — debug / golden-regression mode.
func runCLI(client *linkedin.Client, raw string) {
	typ, slug, err := linkedin.ClassifyURL(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Printf("🔍 fetching %s %q…\n\n", typ, slug)
	var result any
	switch typ {
	case linkedin.URLTypeProfile:
		result, err = client.FetchProfile(slug, "https://www.linkedin.com/in/"+slug+"/")
	case linkedin.URLTypeCompany, linkedin.URLTypeSchool:
		result, err = client.GetCompany(slug, typ.String())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
	file := typ.String() + "_output.json"
	os.WriteFile(file, out, 0644)
	fmt.Printf("\n💾 saved → %s\n", file)
}
