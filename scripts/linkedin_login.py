#!/usr/bin/env python3
"""Cold-path session issuer: births a LinkedIn session in a REAL browser
and saves the FULL cookie jar as a Playwright storage_state file.

Why this exists (docs/recon.md, round 7): LinkedIn kills sessions REPLAYED
from a context that doesn't match the session's birth device dossier. So
the session must be BORN in a real browser whose fingerprint our warm-path
HTTP client (Go tls-client, Chrome profile) closely matches.

Two rules this script is built around:

  1. PERSISTENT profile (scripts/.browser-profile). The SAME browser
     identity must survive across refreshes — importing cookies into a
     FRESH context is exactly what gets other tools auth-walled
     (github.com/stickerdaniel/linkedin-mcp-server#330).
  2. REAL Chrome when available (channel="chrome"): the account dossier
     then says the same Chrome major our warm-path headers claim
     (chromeVersion in internal/linkedin/client.go). Falls back to the
     bundled Chromium with a loud note if Chrome isn't installed.

Usage (Playwright required — the linkedin_scraper project's venv has it):

  ../linkedin_scraper/.venv/bin/python scripts/linkedin_login.py [outfile]

  1. A real browser window opens on linkedin.com/login
  2. Log in MANUALLY (password, 2FA, captcha — take your time, 10 min budget)
  3. The script notices the authenticated session, saves the full jar
     (li_at, JSESSIONID, bcookie, bscookie, li_rm, lidc, ...) and exits

Default outfile: ../linkedin_session.json — the API's default
LINKEDIN_SESSION_FILE (kept one level up = outside the repo).
It OVERWRITES whatever is there — that is the point (fresh birth).

Operating rules for the account's sake: don't browse LinkedIn in another
window while this runs (overlapping sessions are a flag), and re-run this
only when the warm path actually 401s — every fresh birth is a dossier
event, so don't churn it.
"""

import sys
import time
from pathlib import Path

from playwright.sync_api import sync_playwright

REPO_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_OUT = REPO_ROOT.parent / "linkedin_session.json"
PROFILE_DIR = Path(__file__).resolve().parent / ".browser-profile"
LOGIN_URL = "https://www.linkedin.com/login"
POLL_SECONDS = 2
TIMEOUT_SECONDS = 600


def main() -> None:
    out = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else DEFAULT_OUT
    PROFILE_DIR.mkdir(parents=True, exist_ok=True)

    with sync_playwright() as pw:
        # Real Chrome first (matches the dossier our warm path claims);
        # bundled Chromium as fallback (then its major is what client.go's
        # chromeVersion must say — the script prints it).
        try:
            ctx = pw.chromium.launch_persistent_context(
                user_data_dir=str(PROFILE_DIR),
                channel="chrome",
                headless=False,
                no_viewport=True,
            )
            print("browser: real Chrome (channel=chrome)")
        except Exception as e:
            print(f"WARNING: real Chrome unavailable ({e}); using bundled Chromium")
            ctx = pw.chromium.launch_persistent_context(
                user_data_dir=str(PROFILE_DIR),
                headless=False,
                no_viewport=True,
            )
            print(f"browser: bundled Chromium {ctx.browser.version} — "
                    "set chromeVersion in internal/linkedin/client.go to THIS major!")

        page = ctx.pages[0] if ctx.pages else ctx.new_page()
        page.goto(LOGIN_URL, wait_until="domcontentloaded")
        print("log in in the browser window (password/2FA/captcha — no rush)…")

        deadline = time.time() + TIMEOUT_SECONDS
        while time.time() < deadline:
            cookies = ctx.cookies("https://www.linkedin.com")
            names = {c["name"] for c in cookies}
            if "li_at" in names and "JSESSIONID" in names:
                ctx.storage_state(path=str(out))
                print(f"\nsession saved → {out}")
                print(f"cookies captured: {sorted(names)}")
                print("birth complete. Warm path (the Go API) can use this jar now.")
                ctx.close()
                return
            time.sleep(POLL_SECONDS)

        print("timed out waiting for login (10 min) — nothing saved", file=sys.stderr)
        ctx.close()
        sys.exit(1)


if __name__ == "__main__":
    main()
