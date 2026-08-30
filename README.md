# LinkedIn Profile API

HTTP API that takes a LinkedIn URL and returns structured JSON. Works with
profiles (`/in/`), companies (`/company/`) and schools (`/school/`).

No browser involved. The server replays the same internal requests LinkedIn's
own web app makes (Voyager GraphQL + React Flight streams), authenticated with
your own session cookies. The TLS/HTTP2 layer impersonates real Chrome
(bogdanfinn/tls-client — LinkedIn fingerprints the handshake before it reads
a single header, and Go's stock ClientHello is a world-famous bot tell);
above that, two small dependencies (tls-client, godotenv), everything
else is the standard library. How those requests were identified:
[docs/recon.md](docs/recon.md).

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

Request pacing: a profile fetch is 3 jittered calls (topcard → dash →
contact overlay) with fresh per-page-load tracking IDs — a few seconds on a
cache miss. Fewer requests = lower velocity score; every call is one the
real web app actually makes. `LINKEDIN_PACING=fast` disables the pauses for
local development; don't use it against a real account you care about.

## API

### `GET /v1/profile?url=<linkedin-url>`

```bash
curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/maitrey-trivedi-theta-technolabs/"
```

Profile response, `200 OK` (real, unedited output — this profile populates
every field):

```json
{
  "name": "Maitrey Trivedi",
  "headline": "CEO @ Theta Technolabs ↑ || Most Companies Don’t Need More Tech, they Need Better Systems || Building Scalable AI, IoT & Blockchain Systems",
  "location": "Ahmedabad, Gujarat, India",
  "experience": [
    {
      "title": "CEO and Director of Sales and Growth",
      "company": "Theta Technolabs",
      "employment_type": "Full-time",
      "date_range": "Jan 2023 - Present",
      "from": "Jan 2023",
      "to": "Present",
      "location": "Ahmedabad, Gujarat, India"
    },
    {
      "title": "Founder",
      "company": "Theta Technolabs",
      "employment_type": "Full-time",
      "date_range": "Sep 2015 - Present",
      "from": "Sep 2015",
      "to": "Present",
      "location": "India"
    }
  ],
  "education": [
    {
      "school": "Gujarat Technological University (GTU)",
      "degree": "Master of Computer Applications (MCA), iPhone iPad Applications",
      "date_range": "2010 – 2013",
      "from": "2010",
      "to": "2013"
    },
    {
      "school": "Gujarat University",
      "degree": "Bachelor in Computer Applications, VB.Net",
      "date_range": "2007 – 2010",
      "from": "2007",
      "to": "2010"
    }
  ],
  "skills": ["Sales Operations", "Marketing"],
  "certifications": [
    {
      "title": "Lead Generation & AI Tool",
      "issuer": "IT Sales Community",
      "issued_date": "Oct 2024"
    }
  ],
  "languages": [
    {"name": "English", "proficiency": "Professional working proficiency"},
    {"name": "Gujarati", "proficiency": "Native or bilingual proficiency"}
  ],
  "recommendations": [],
  "contact_info": {
    "websites": ["thetatechnolabs.com"]
  },
  "profile_images": [
    "https://media.licdn.com/dms/image/v2/D5603AQF63Vx4Mu8b7Q/profile-displayphoto-scale_400_400/..."
  ],
  "linkedin_url": "https://www.linkedin.com/in/maitrey-trivedi-theta-technolabs/"
}
```

Field notes:

- `about`: omitted when the profile has no About section (this one doesn't).
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
401  linkedin session expired — refresh LI_AT/JSESSIONID cookies
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

- Auth is a logged-in session's cookies. If the session expires or LinkedIn
  restricts the account, profile fetches fail until fresh cookies are set
  (`/healthz` stays green — it only checks the process, not the session).
- All upstream calls go out from one account, so volume is deliberately
  limited: a global fetch cap (429 + Retry-After when full), a 24h response
  cache, singleflight coalescing, and human-paced request waves. Scaling
  past that means a pool of sessions, not more per-account volume.
- Section parsing anchors on English text in LinkedIn's internal stream
  format, which LinkedIn can change at any time. The defense is validation
  (empty name/sections = a loud 502, never silently wrong data) plus
  golden-profile regression tests.
- Email and phone in `contact_info` are only returned when the target shares
  them with the authenticated account — typically 1st-degree connections.
- Connection counts are not served for other members' profiles; companies
  return `followers`, personal profiles do not.
