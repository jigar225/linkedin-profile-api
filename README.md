# LinkedIn Profile API

HTTP API that takes a LinkedIn URL and returns structured JSON. Works with
profiles (`/in/`), companies (`/company/`) and schools (`/school/`).

No browser at serve time. The session is BORN once in a real browser
(cold path — `scripts/linkedin_login.py`), then a pure-Go client replays
the same internal requests LinkedIn's own web app makes: 3 calls per
profile (Voyager GraphQL topcard → Voyager dash full profile → RSC contact
overlay), authenticated with the browser's full cookie jar. The TLS/HTTP2
layer impersonates real Chrome (bogdanfinn/tls-client — LinkedIn
fingerprints the handshake before it reads a single header, and Go's stock
ClientHello is a world-famous bot tell). Direct dependencies: tls-client,
godotenv. How those requests were identified — and why sessions used to
die: [docs/recon.md](docs/recon.md).

## Run

Requires Go 1.24+ and a browser-born LinkedIn session.

**Primary auth — the full cookie jar (strongly preferred).** LinkedIn kills
sessions replayed from a context that doesn't match the birth device dossier
(docs/recon.md), so the session must be BORN in a real browser and used with
its WHOLE jar (`bcookie` is LinkedIn's device ID — the dossier expects it).
The cold-path script does the birth:

```bash
# needs Playwright (pip install playwright && playwright install chromium)
python3 scripts/linkedin_login.py   # real browser opens → log in manually
go run ./cmd                        # picks up ../linkedin_session.json
```

It writes a Playwright `storage_state` JSON to `../linkedin_session.json`
(override path: `LINKEDIN_SESSION_FILE`). Re-run it only when the API
starts returning 401 (session dead) — days of life per birth is normal.

**Fallback auth — stripped jar.** Two cookies in env (`.env` supported, see
`.env.example`): `LI_AT` + `JSESSIONID` from DevTools -> Application ->
Cookies. Works, but lacks the device cookies — use the session file
whenever possible.

## Deploy (cloud)

The browser exists ONLY at birth time, on your local machine — the server
never runs Python or Chrome. The Dockerfile (pure Go, static binary) is all
the cloud needs:

```bash
# locally: birth the session (real Chrome, manual login)
python3 scripts/linkedin_login.py        # writes ../linkedin_session.json

# cloud: ship the jar as an env var + a residential proxy
LINKEDIN_SESSION_JSON='<contents of ../linkedin_session.json>'
LINKEDIN_PROXY='http://user:pass@your-residential-proxy:8080'
docker build -t linkedin-profile-api . && docker run -p 8080:8080 \
  -e LINKEDIN_SESSION_JSON -e LINKEDIN_PROXY linkedin-profile-api
```

Two deploy rules, both evidence-backed (docs/recon.md): (1) the jar must be
the FULL browser-born jar, re-birthed locally when the API starts 401ing —
no Python/Chrome on the server, ever; (2) `LINKEDIN_PROXY` is effectively
REQUIRED on cloud — a session born on a home IP but used from a datacenter
egress is the #1 session-kill pattern ("impossible travel"). Match the
proxy's geography to the birth machine's.

⚠️ The API has NO auth of its own. Anyone who finds your deployment URL can
spend your LinkedIn account's daily budget (and get it flagged). Keep the
URL private, or front it with a proxy that adds auth.

Request pacing: a profile fetch is 3 jittered calls (topcard → dash →
contact overlay) with fresh per-page-load tracking IDs — a few seconds on a
cache miss. Fewer requests = lower velocity score; every call is one the
real web app actually makes. `LINKEDIN_PACING=fast` disables the pauses for
local development; don't use it against a real account you care about.

## API

### `GET /v1/profile?url=<linkedin-url>`

```bash
curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/bibek-ranjan-saha/"
```

Profile response, `200 OK` (real, unedited output from the live deployment —
trimmed where marked):

```json
{
  "name": "Bibek saha",
  "first_name": "Bibek",
  "last_name": "saha",
  "headline": "Full Stack & Mobile App Developer | Flutter, Java, React Native | ...",
  "location": "Mumbai Metropolitan Region, India",
  "country": "India",
  "country_iso": "IN",
  "public_identifier": "bibek-ranjan-saha",
  "profile_urn": "urn:li:fsd_profile:ACoAAC6vFiwBoVYUHgCm7xpFjJKezHwHHz8UXUk",
  "member_urn": "urn:li:member:783226412",
  "premium": false,
  "creator": true,
  "influencer": false,
  "profile_created_at": "2019-12-16",
  "locale": "en_US",
  "about": "I am a Software Engineer with 3+ years of experience building cross-platform mobile applications...",
  "experience": [
    {
      "title": "Flutter developer",
      "company": "Skillmine Technology",
      "employment_type": "Full-time",
      "date_range": "Jun 2026 - Present",
      "from": "Jun 2026",
      "to": "Present",
      "location": "Thane, Maharashtra, India"
    },
    {
      "title": "Mobile Application Developer",
      "company": "Boon.ai",
      "employment_type": "Full-time",
      "date_range": "Apr 2025 - May 2026",
      "from": "Apr 2025",
      "to": "May 2026",
      "location": "Hyderabad"
    }
  ],
  "education": [
    {
      "school": "Gandhi Institute of Engineering and Technology (GIET), Gunupur",
      "degree": "Bachelor of Technology - BTech, Computer Science",
      "date_range": "2019 - 2023",
      "from": "2019",
      "to": "2023"
    }
  ],
  "skills": ["Application Development", "Application Architecture", "..."],
  "certifications": [
    {
      "title": "Google Play Academy - Store Listing Certificate",
      "issuer": "Google Play",
      "issued_date": "Aug 2025"
    }
  ],
  "languages": [
    {"name": "English", "proficiency": "Full professional proficiency"},
    {"name": "Hindi", "proficiency": "Native or bilingual proficiency"}
  ],
  "recommendations": [],
  "contact_info": {
    "websites": ["bibek-saha.web.app"]
  },
  "profile_images": ["https://media.licdn.com/dms/image/v2/D5603AQHyDvWXmSqDfg/profile-displayphoto-scale_400_400/..."],
  "cover_images": ["https://media.licdn.com/dms/image/v2/D4D16AQE6Glqye3OJ-w/profile-displaybackgroundimage-shrink_350_1400/..."],
  "profile_image_alt": "Bibek saha",
  "profile_image_ai_generated": false,
  "relationship_status": "not_connected",
  "network_distance": "OUT_OF_NETWORK",
  "invitation_status": "none",
  "linkedin_url": "https://www.linkedin.com/in/bibek-ranjan-saha/"
}
```

Field notes:

- Identity block: `profile_urn`/`member_urn` are LinkedIn's internal IDs;
  `premium`/`creator`/`influencer` are the account badges;
  `profile_created_at` is the member-since date (ISO).
- `relationship_status`: `self` | `connected` | `not_connected` — how the
  VIEWING account relates to this profile; `network_distance` e.g.
  `OUT_OF_NETWORK`; `invitation_status`: `none` | `pending`.
- `about`: omitted when the profile has no About section.
- `languages`: `proficiency` is omitted when the profile doesn't state one.
- `recommendations`: currently always empty — both sources were
  session-kill suspects and are retired (recoverable via git history).
  The field stays in the schema so consumers don't break.
- `contact_info`: omitted entirely when the member shares nothing with your
  account. Email/phone are usually only visible for 1st-degree connections;
  websites/socials sometimes show for anyone.

Same endpoint for companies and schools
(`/v1/profile?url=https://www.linkedin.com/company/nvidia/`):

```json
{
  "type": "company",
  "name": "NVIDIA",
  "description": "Since its founding in 1993, NVIDIA (NASDAQ: NVDA) has been a pioneer...",
  "industry": "Computer Hardware Manufacturing",
  "website": "http://www.nvidia.com",
  "location": "Santa Clara, CA, US",
  "founded": 1993,
  "staff_count": 51643,
  "staff_range": "10001+",
  "followers": 37247767,
  "specialties": ["GPU-accelerated computing", "artificial intelligence"],
  "logo_urls": ["https://media.licdn.com/dms/image/..."],
  "cover_urls": ["https://media.licdn.com/dms/image/..."],
  "linkedin_url": "https://www.linkedin.com/company/nvidia/"
}
```

Errors, always `{"error": "..."}`:

```
400  missing "url" query parameter
400  unsupported LinkedIn URL type (response includes the supported list:
     /in/, /company/, /school/)
401  linkedin session expired — re-run scripts/linkedin_login.py
404  profile not found (or not visible to this account)
429  too many upstream fetches in flight (Retry-After header set)
502  upstream LinkedIn failure — retry shortly
```

Responses carry `X-Cache: hit` when served from cache, `X-Cache: stale` when
the upstream fetch failed and the last good response was served instead.

### `GET /healthz`

```json
{"status": "ok"}
```

## Known limitations

- Auth is a browser-born session's cookie jar. When LinkedIn kills it
  (401s), fetches fail until the session is re-birthed locally
  (`scripts/linkedin_login.py`) — `/healthz` stays green regardless, it
  only checks the process.
- All upstream calls go out from one account, so volume is deliberately
  limited: a global fetch cap (429 + Retry-After when full), a 24h response
  cache, singleflight coalescing, and jittered human pacing (3 calls per
  profile). Scaling past that means a pool of sessions + residential IPs,
  not more per-account volume (community-verified safe budget:
  ~10-20 profiles/day/account).
- Sections come from LinkedIn's typed dash entities (structured dates,
  enums) — layout-independent, but LinkedIn can still change or retire the
  decoration at any time. The defense is validation (empty name/sections =
  a loud 502, never silently wrong data) plus parser unit tests.
- Email and phone in `contact_info` are only returned when the target shares
  them with the authenticated account — typically 1st-degree connections.
- Connection counts are not served for other members' profiles; companies
  return `followers`, personal profiles do not.
