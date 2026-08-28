# LinkedIn Profile API

HTTP API that takes a LinkedIn URL and returns structured JSON. Works with
profiles (`/in/`), companies (`/company/`) and schools (`/school/`).

No browser involved. The server replays the same internal requests LinkedIn's
own web app makes (Voyager GraphQL + React Flight streams), authenticated with
your own session cookies. Pure Go — one small dependency (godotenv), everything
else is the standard library.

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

## API

### `GET /v1/profile?url=<linkedin-url>`

```bash
curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/islaniaaayush/"
```

Profile response, `200 OK`:

```json
{
  "name": "Aayush Islania",
  "headline": "Cloud & Security Engineer | Microsoft 365 Migration | Endpoint Security | BitDefender",
  "location": "Ahmedabad, Gujarat, India",
  "about": "I'm a Cloud & Security Engineer currently working at Teqtive Solutions...",
  "experience": [
    {
      "title": "Technical Engineer - Cloud and Endpoint security",
      "company": "Teqtive Solutions",
      "employment_type": "Full-time",
      "date_range": "Dec 2025 - Present",
      "from": "Dec 2025",
      "to": "Present"
    }
  ],
  "education": [
    {
      "school": "SAL INSTITUTE OF TECH. & ENGG. RESEARCH, AHMEDABAD 067",
      "degree": "Bachelor of Engineering - BE, Computer Science",
      "date_range": "2021 – 2025",
      "from": "2021",
      "to": "2025"
    }
  ],
  "skills": ["Bitdefender Endpoint Protection", "Microsoft 365"],
  "certifications": [
    {
      "title": "Introduction to Cybersecurity",
      "issuer": "Cisco",
      "issued_date": "Nov 2025"
    }
  ],
  "languages": [],
  "profile_images": [
    "https://media.licdn.com/dms/image/v2/D4D03AQFP5AfItO2rGw/profile-displayphoto-shrink_400_400/..."
  ],
  "linkedin_url": "https://www.linkedin.com/in/islaniaaayush/"
}
```

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
502  upstream LinkedIn failure (session expired, rate limited, not found)
```

### `GET /healthz`

```json
{"status": "ok"}
```
