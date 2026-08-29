# LinkedIn Profile API

HTTP API that takes a LinkedIn URL and returns structured JSON. Works with
profiles (`/in/`), companies (`/company/`) and schools (`/school/`).

No browser involved. The server replays the same internal requests LinkedIn's
own web app makes (Voyager GraphQL + React Flight streams), authenticated with
your own session cookies. Pure Go — one small dependency (godotenv), everything
else is the standard library. How those requests were identified:
[docs/recon.md](docs/recon.md).

## Run

Requires Go 1.22+ and two cookies from your own LinkedIn session.

Getting the cookies: log into linkedin.com, open DevTools (F12) ->
Application -> Storage -> Cookies -> `https://www.linkedin.com`, and copy the
values of `li_at` and `JSESSIONID`. Note that `li_at` is HttpOnly, so
`document.cookie` can't see it — you need the Application tab. Quotes around
the JSESSIONID value are optional.

```bash
export LI_AT='your-li_at-value'
export JSESSIONID='ajax:your-jsessionid-value'
go run ./cmd          # listens on :8080, override with PORT
```

Or with a `.env` file (see `.env.example`):

```bash
cp .env.example .env   # fill in your values
go run ./cmd           # .env is loaded automatically
```

Alternative auth: point `LINKEDIN_SESSION_FILE` at a Playwright
`storage_state` JSON. Used only when `LI_AT`/`JSESSIONID` are not set.

Request pacing: by default the server shapes its upstream calls like the real
LinkedIn web app (sections fetched in waves with pauses, the contact overlay
trailing last like a clicked overlay, not all at once) — a profile fetch
takes ~13-18s on a cache miss. `LINKEDIN_PACING=fast`
disables the pauses for local development; don't use it against a real
account you care about.

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
  "recommendations": [
    {
      "recommender": "Sunny Vaghadia",
      "headline": "Expert Engineer at Apexon | JavaScript | React JS | TypeScript | ...",
      "relationship": "Maitrey worked with Sunny but on different teams",
      "date": "June 17, 2025",
      "text": "I had the pleasure of working with him for a couple of years at Vivacious Websolution...",
      "direction": "received"
    }
  ],
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
- `recommendations[].direction`: `"received"` or `"given"`, read from the
  section's Received/Given toggle state in the stream. Omitted when the
  stream carries no toggle state — we don't guess. The `relationship` line
  always names both parties either way ("Maitrey managed Saumya directly").
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
429  too many upstream fetches in flight (Retry-After header set)
502  upstream LinkedIn failure (session expired, rate limited, not found)
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
