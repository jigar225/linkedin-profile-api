// LinkedIn Profile API — reverse-engineered, browserless.
//
// Server mode (default):  go run ./cmd                       → serves /v1/profile?url=...
// CLI mode (debug):       go run ./cmd -url <linkedin-url>   → one-shot fetch, prints JSON
package main

import (
	"encoding/json"
	"flag"
	"fmt"
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

func main() {
	// Load .env for local dev if present. Missing file is fine (prod injects
	// real env vars); existing env vars always win. Must run before anything
	// reads the environment — flag defaults included.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "warning: .env:", err)
	}

	url := flag.String("url", "", "one-shot CLI mode: fetch this LinkedIn URL and print JSON")
	session := flag.String("session",
		envOr("LINKEDIN_SESSION_FILE", "../linkedin_session.json"),
		"path to session cookies JSON (env: LINKEDIN_SESSION_FILE; default assumes repo root, file kept one level up = outside the repo)")
	flag.Parse()

	// Auth resolution: env cookies (prod / repo users) win over the session
	// file (our dev flow). Built once — server handlers reuse the client
	// (connection pooling) across requests.
	var client *linkedin.Client
	liAt, jsessionID := os.Getenv("LI_AT"), os.Getenv("JSESSIONID")
	switch {
	case liAt != "" && jsessionID != "":
		client = linkedin.NewClient(liAt, jsessionID)
		fmt.Fprintln(os.Stderr, "🔑 auth: LI_AT + JSESSIONID env cookies")
	case liAt != "" || jsessionID != "":
		fmt.Fprintln(os.Stderr, "error: set BOTH LI_AT and JSESSIONID, or neither (falls back to session file)")
		os.Exit(1)
	default:
		var err error
		client, err = linkedin.NewClientFromSessionFile(*session)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "🔑 auth: session file", *session)
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
