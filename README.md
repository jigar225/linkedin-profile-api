# LinkedIn Profile API

Give it a LinkedIn URL, it gives back structured JSON. Works with profiles
(`/in/`), companies (`/company/`) and schools (`/school/`).

No browser is involved when the server runs. The Go client makes the same
internal requests that LinkedIn's own website makes — 3 per profile — using
the cookies from one real browser login. It also copies Chrome's TLS
fingerprint, because LinkedIn checks the TLS handshake before it reads a
single header, and Go's default handshake is a well-known bot giveaway.
How I found these requests (with evidence): [docs/recon.md](docs/recon.md).

31 fields per profile · 14 per company · 3 requests to LinkedIn per profile ·
24h cache

## Quickstart

You need Go 1.24+ and a LinkedIn account you're OK using for this.

1. Log in once, to create the session:

   ```bash
   pip install playwright && playwright install chromium
   python3 scripts/linkedin_login.py   # a real Chrome window opens → log in manually
   ```

   This saves your LinkedIn cookies to `../linkedin_session.json` (outside
   the repo on purpose, so secrets can't be committed). You only need to do
   this again when the API starts returning 401 — a session usually lives
   for days.

2. Run the server:

   ```bash
   go run ./cmd    # serves :8080
   ```

3. Fetch:

   ```bash
   curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/marcrandolph/"
   ```

One-shot CLI mode (no server): `go run ./cmd -url <linkedin-url>` — prints
the JSON and saves it to `<type>_output.json` so you can diff two runs.

Auth is resolved in this order:

| Priority | Source | Notes |
|---|---|---|
| 1 | `LINKEDIN_SESSION_JSON` env — the whole cookie jar as a string | for cloud deploys |
| 2 | the session file (default `../linkedin_session.json`, override with `LINKEDIN_SESSION_FILE`) | local default |
| 3 | `LI_AT` + `JSESSIONID` env (`.env` supported) | fallback — works, but it's missing the device cookies (`bcookie` is how LinkedIn knows your machine), so prefer the full jar |

Why the full jar? LinkedIn doesn't just check that your cookies are valid —
it checks that they show up from a place that looks like the machine they
were created on. That's why the session has to come from a real browser
login, and why a cloud server needs a proxy (more below). Evidence:
[docs/recon.md](docs/recon.md). Full env list:
[.env.example](.env.example).

## What you get

Real, unedited response for `linkedin.com/in/marcrandolph`. The image URLs
are signed by LinkedIn's CDN and expire, so they're cut short with `…`
here:

```json
{
  "name": "Marc Randolph",
  "first_name": "Marc",
  "last_name": "Randolph",
  "headline": "Netflix Co-Founder, Entrepreneur, Mentor & Investor",
  "location": "Santa Cruz, California, United States",
  "country": "United States",
  "country_iso": "US",
  "public_identifier": "marcrandolph",
  "profile_urn": "urn:li:fsd_profile:ACoAAAESBRABM_oNqcEYjHrVQLl74riMx8N9Sd0",
  "member_urn": "urn:li:member:17958160",
  "premium": true,
  "creator": true,
  "influencer": true,
  "profile_created_at": "2007-11-12",
  "locale": "en_US",
  "about": "Marc Randolph is a veteran Silicon Valley entrepreneur, advisor and investor.\n\nAlthough best known as the co-founder and first CEO of Netflix, Marc’s career as an entrepreneur spans more than four decades.  He's founded or co-founded more than half a dozen other successful start-ups, mentored rising entrepreneurs including the co-founders of Looker Data which was recently sold to Google for $2.6B, and invested in numerous successful tech ventures.\n\nHe is a frequent speaker at industry events, works extensively with young entrepreneur programs, and helps numerous companies and non-profits, serving variously as a board member, mentor, or executive coach. \n\nMarc is the author of an international bestselling memoir, and the host of the new podcast, That Will Never Work where he dispenses advice, encouragement and tough love to aspiring entrepreneurs.",
  "experience": [
    {
      "title": "Board Member",
      "company": "Cheeze, Inc.",
      "date_range": "May 2022 - Present",
      "from": "May 2022",
      "to": "Present"
    },
    {
      "title": "Board Member",
      "company": "Truckee Donner Land Trust",
      "date_range": "Nov 2021 - Present",
      "from": "Nov 2021",
      "to": "Present"
    },
    {
      "title": "Chief Executive Officer",
      "company": "PodiumCraft",
      "date_range": "Jan 2010 - Present",
      "from": "Jan 2010",
      "to": "Present",
      "location": "Santa Cruz, California, United States"
    },
    {
      "title": "Board Member",
      "company": "Solo Brands",
      "date_range": "Sep 2021 - Aug 2024",
      "from": "Sep 2021",
      "to": "Aug 2024"
    },
    {
      "title": "Board Member",
      "company": "Hamilton College",
      "date_range": "Jun 2022 - Jul 2023",
      "from": "Jun 2022",
      "to": "Jul 2023"
    },
    {
      "title": "Board Member",
      "company": "Augment CXM",
      "date_range": "Feb 2017 - Feb 2022",
      "from": "Feb 2017",
      "to": "Feb 2022"
    },
    {
      "title": "Board Member",
      "company": "Dishcraft ",
      "date_range": "Feb 2016 - Jan 2022",
      "from": "Feb 2016",
      "to": "Jan 2022"
    },
    {
      "title": "Board Member",
      "company": "1% for the Planet",
      "date_range": "Oct 2015 - Sep 2021",
      "from": "Oct 2015",
      "to": "Sep 2021",
      "location": "Burlington, Vermont Area"
    },
    {
      "title": "Board Member",
      "company": "Chubbies Shorts",
      "date_range": "Feb 2014 - Jul 2021",
      "from": "Feb 2014",
      "to": "Jul 2021"
    },
    {
      "title": "Board Member",
      "company": "National Outdoor Leadership School",
      "date_range": "Jun 2011 - Jun 2020",
      "from": "Jun 2011",
      "to": "Jun 2020"
    }
  ],
  "education": [
    {
      "school": "Hamilton College",
      "degree": "Bachelor of Arts (B.A.), Geology",
      "date_range": "1976 - 1981",
      "from": "1976",
      "to": "1981"
    }
  ],
  "skills": [
    "Startups"
  ],
  "certifications": [],
  "languages": [],
  "recommendations": [],
  "contact_info": {
    "websites": [
      "marcrandolph.com"
    ]
  },
  "profile_images": [
    "https://media.licdn.com/dms/image/v2/D5603AQF7oF2MnpG-rg/profile-displayphoto-shrink_100_100/…",
    "https://media.licdn.com/dms/image/v2/D5603AQF7oF2MnpG-rg/profile-displayphoto-shrink_200_200/…",
    "https://media.licdn.com/dms/image/v2/D5603AQF7oF2MnpG-rg/profile-displayphoto-shrink_400_400/…",
    "https://media.licdn.com/dms/image/v2/D5603AQF7oF2MnpG-rg/profile-displayphoto-shrink_800_800/…"
  ],
  "cover_images": [
    "https://media.licdn.com/dms/image/v2/C5616AQEPomWx1Jq_Ow/profile-displaybackgroundimage-shrink_200_800/…",
    "https://media.licdn.com/dms/image/v2/C5616AQEPomWx1Jq_Ow/profile-displaybackgroundimage-shrink_350_1400/…"
  ],
  "profile_image_alt": "Marc Randolph",
  "profile_image_ai_generated": false,
  "relationship_status": "not_connected",
  "network_distance": "OUT_OF_NETWORK",
  "invitation_status": "none",
  "linkedin_url": "https://www.linkedin.com/in/marcrandolph/"
}
```

### Profile fields — identity

| Field | Description |
|---|---|
| `name`, `first_name`, `last_name` | full name and its parts |
| `headline` | the professional headline |
| `location` | city/region/country as written on the profile |
| `country`, `country_iso` | resolved country + 2-letter ISO code |
| `public_identifier` | the `/in/` slug |
| `profile_urn`, `member_urn` | LinkedIn's internal IDs |
| `premium`, `creator`, `influencer` | account badges — always present |
| `profile_created_at` | when the member joined LinkedIn (YYYY-MM-DD) |
| `locale` | the language the member uses LinkedIn in (e.g. `en_US`) |
| `linkedin_url` | the canonical profile URL |

### Profile fields — sections

| Field | Description |
|---|---|
| `about` | left out when the profile has no About section |
| `experience[]` | `title`, `company`, `employment_type`, `date_range` + parsed `from`/`to`, `location` — any of these is left out if LinkedIn didn't return it |
| `education[]` | `school`, `degree`, `date_range` + parsed `from`/`to` |
| `skills[]` | skill names |
| `certifications[]` | `title`, `issuer`, `issued_date` |
| `languages[]` | `name` + `proficiency` (left out when the profile doesn't state one) |
| `recommendations[]` | always empty for now — both ways of fetching it tripped LinkedIn's anti-abuse systems, so they were removed (git history has the code). The field stays so your parsing doesn't break |
| `contact_info` | `email`, `phone`, `birthday`, `twitter`, `websites[]` — the whole object is left out when the member shares nothing with your account (normal for 3rd-degree profiles); email/phone usually only show for 1st-degree connections |

### Profile fields — media

| Field | Description |
|---|---|
| `profile_images[]` | every photo size LinkedIn serves (100 → 800px) |
| `cover_images[]` | background banner sizes |
| `profile_image_alt` | the photo's alt text |
| `profile_image_ai_generated` | LinkedIn's flag for AI-generated photos |

### Profile fields — relationship

These describe how the account you're scraping with relates to this
profile:

| Field | Description |
|---|---|
| `relationship_status` | `self` \| `connected` \| `not_connected` |
| `network_distance` | `DISTANCE_1`–`DISTANCE_3`, or `OUT_OF_NETWORK` |
| `invitation_status` | `none` \| `pending` |

### Companies and schools

`GET /v1/profile?url=https://www.linkedin.com/company/nvidia/`:

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
  "logo_urls": ["https://media.licdn.com/dms/image/…"],
  "cover_urls": ["https://media.licdn.com/dms/image/…"],
  "linkedin_url": "https://www.linkedin.com/company/nvidia/"
}
```

| Field | Description |
|---|---|
| `type` | `company` or `school` — both go through the same endpoint |
| `description` | the full About text |
| `industry` | LinkedIn's industry name for the company |
| `location` | HQ — city, region, country |
| `staff_count`, `staff_range` | exact employee count + LinkedIn's size bucket |
| `followers` | page followers (companies only — personal profiles don't expose one) |
| `logo_urls[]`, `cover_urls[]` | every size LinkedIn serves |

## API reference

### `GET /v1/profile?url=<linkedin-url>`

| Parameter | Required | Description |
|---|---|---|
| `url` | yes | LinkedIn `/in/`, `/company/` or `/school/` URL. Works without the `https://` part too, and ignores trailing slashes and query strings. Any other LinkedIn URL type → 400 with the list of what's supported. |

Errors — always `{"error": "..."}`:

| Status | Meaning |
|---|---|
| 400 | missing `url` parameter, or an unsupported LinkedIn URL type |
| 401 | the LinkedIn session died — run `scripts/linkedin_login.py` again |
| 404 | profile not found (or not visible to your account) |
| 429 | too many live fetches at once — wait the `Retry-After` seconds and try again |
| 502 | a LinkedIn call failed — try again shortly |

Cache headers:

| Header | Meaning |
|---|---|
| `X-Cache: hit` | served from the 24h cache — LinkedIn was not called |
| `X-Cache: stale` | the live fetch failed, so the last good response was served instead |
| (absent) | fresh fetch, straight from LinkedIn |

### `GET /healthz`

```json
{"status": "ok"}
```

Only tells you the server is up — it does not check whether the LinkedIn
session is still alive.

## How it works

A profile fetch makes 3 requests to LinkedIn — the same 3 its own website
makes when someone opens a profile:

1. **Profile card** (Voyager GraphQL) — name, headline, location, IDs,
   badges, photos.
2. **Full profile** (Voyager dash) — experience, education, skills,
   certifications and languages as proper structured data (real dates, not
   text scraped off the page). This is why a LinkedIn page redesign doesn't
   break the parsing.
3. **Contact-info popup** (the "Contact info" overlay) — whatever the
   member shares with the viewing account: websites, sometimes email/phone.

Every request goes out looking like Chrome
([bogdanfinn/tls-client](https://github.com/bogdanfinn/tls-client)), with
fresh tracking IDs and small random delays between calls — so a fetch takes
a few seconds when it's not cached. Responses are checked before they're
served: if the name is empty or every section came back empty, you get a
loud 502, never silently wrong data.

Direct dependencies: tls-client, godotenv. Nothing else.

## Deploy (cloud)

The browser is only needed once, on your own machine, to log in and create
the session. The server itself never runs Python or Chrome — the Dockerfile
builds a small pure-Go binary, and that's all the cloud needs.

1. Create the session locally: `python3 scripts/linkedin_login.py`
2. Set two environment variables on your platform:

   ```
   LINKEDIN_SESSION_JSON=<contents of ../linkedin_session.json>
   LINKEDIN_PROXY=http://user:pass@your-residential-proxy:port
   ```

3. Build and run:

   ```bash
   docker build -t linkedin-profile-api .
   docker run -p 8080:8080 -e LINKEDIN_SESSION_JSON -e LINKEDIN_PROXY linkedin-profile-api
   ```

Two deploy rules, both learned the hard way ([docs/recon.md](docs/recon.md)):

- Always use the full cookie jar from the login script. When the API starts
  returning 401, run the script again on your machine — never try to log in
  from the server.
- `LINKEDIN_PROXY` is basically required on cloud. A session created on a
  home connection suddenly showing up from a datacenter IP is the fastest
  way to get it killed — that's exactly what happened to our first
  deployment ([docs/logs.md](docs/logs.md)). Use a static residential proxy
  in the same country you logged in from.

**The API has no auth of its own.** Anyone who finds your deployment URL
can spend your LinkedIn account's daily budget (and get the account
flagged). Keep the URL private, or put it behind a proxy that adds auth.

## Pacing and account safety

Everything goes out from one LinkedIn account, so the server is careful
about volume:

- 3 requests per profile, with small random delays between them
  (`LINKEDIN_PACING=fast` turns the delays off — fine for local testing,
  don't use it on an account you care about)
- 24h cache — asking for the same profile again costs nothing
- if 10 people ask for the same profile at the same time, only 1 request
  goes to LinkedIn
- at most 4 live fetches at a time — anything more gets a 429 with
  `Retry-After`

From our own testing and community reports, ~10-20 profiles/day/account is
the safe budget ([docs/recon.md](docs/recon.md),
[docs/logs.md](docs/logs.md)). To go beyond that you need a pool of
accounts behind residential proxies — not more volume from one account.

## Known limitations

- The session is cookies from a real browser login. When LinkedIn kills it
  (you'll see 401s), fetches fail until you run the login script again.
  `/healthz` stays green either way — it only checks the server is up.
- Profile data comes from LinkedIn's own structured endpoints, so page
  redesigns don't break it — but LinkedIn can still change those endpoints
  anytime. If they do, you get a loud 502 instead of wrong data, and there
  are unit tests over captured responses
  (`internal/linkedin/dash_test.go`).
- `recommendations` is always empty — both ways of fetching it tripped
  LinkedIn's anti-abuse systems, so they were removed. The field stays in
  the response so consumers don't break.
- Email and phone in `contact_info` only show up when the member shares
  them with your account — typically 1st-degree connections.
- LinkedIn doesn't serve connection counts for other people's profiles, so
  profiles don't have one. Companies return `followers`.
