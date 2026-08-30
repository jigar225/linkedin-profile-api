package linkedin

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// TopcardQueryID is LinkedIn's persisted-query id for profile-by-vanity-name.
// Captured from real traffic (network_log.jsonl). If it ever 400s, re-capture.
const TopcardQueryID = "voyagerIdentityDashProfiles.34ead06db82a2cc9a778fac97f69ad6a"

// Topcard is the verified "top of profile" data from Voyager GraphQL.
type Topcard struct {
	FirstName  string
	LastName   string
	Headline   string
	Location   string // resolved from the Geo entity
	Country    string // resolved via the location Geo's *country reference
	CountryISO string // e.g. "US", from the location Geo

	PublicIdentifier string // profile slug (christopher-howie-a2z)
	ProfileURN       string // urn:li:fsd_profile:<id> — needed as vieweeProfileId for RSC calls
	MemberURN        string // urn:li:member:<id> (objectUrn)
	VieweeID         string // just the <id> part

	Premium    bool
	Creator    bool
	Influencer bool
	CreatedAt  string // ISO date (2006-01-02) from the ms-epoch `created` field
	Locale     string // e.g. "en_US", from primaryLocale

	PhotoURLs        []string // profile picture, one URL per size
	CoverURLs        []string // background/cover image sizes
	PhotoAltText     string   // profilePicture.a11yText
	PhotoAIGenerated bool     // profilePicture.isGeneratedOrModifiedByAi

	RelationshipStatus string // self | connected | not_connected
	NetworkDistance    string // e.g. OUT_OF_NETWORK
	InvitationStatus   string // none | pending
}

// voyagerResponse mirrors LinkedIn's normalized JSON envelope: {data, meta, included}.
// Entities live in `included`, typed by "$type" and cross-referenced by URN.
type voyagerResponse struct {
	Included []json.RawMessage `json:"included"`
}

type entity struct {
	Type              string          `json:"$type"`
	EntityURN         string          `json:"entityUrn"`
	PublicIdentifier  string          `json:"publicIdentifier"`
	FirstName         string          `json:"firstName"`
	LastName          string          `json:"lastName"`
	Headline          string          `json:"headline"`
	GeoLocation       json.RawMessage `json:"geoLocation"`
	ProfilePicture    json.RawMessage `json:"profilePicture"`
	BackgroundPicture json.RawMessage `json:"backgroundPicture"`
	// Identity enrichment (flat on the Profile entity)
	ObjectURN             string   `json:"objectUrn"` // urn:li:member:<id>
	Premium               bool     `json:"premium"`
	Creator               bool     `json:"creator"`
	Influencer            bool     `json:"influencer"`
	Created               int64    `json:"created"` // ms epoch
	PrimaryLocale         locale   `json:"primaryLocale"`
	SupportedLocales      []locale `json:"supportedLocales"`
	MemberRelationshipURN string   `json:"*memberRelationship"`
	// Geo fields
	DefaultLocalizedName string `json:"defaultLocalizedName"`
	CountryURN           string `json:"*country"` // location Geo -> country Geo reference
	CountryISOCode       string `json:"countryISOCode"`
}

// locale is LinkedIn's {language, country} pair ("en" + "US" -> "en_US").
type locale struct {
	Language string `json:"language"`
	Country  string `json:"country"`
}

func (l locale) String() string {
	if l.Country == "" {
		return l.Language
	}
	return l.Language + "_" + l.Country
}

// memberRelationshipEntity mirrors the viewer<->viewee relationship entity:
// exactly one of self/connection/noConnection is set.
type memberRelationshipEntity struct {
	MemberRelationship struct {
		Self         json.RawMessage `json:"self"`
		Connection   json.RawMessage `json:"connection"`
		NoConnection *struct {
			Invitation struct {
				NoInvitation json.RawMessage `json:"noInvitation"`
				Invitation   json.RawMessage `json:"invitation"`
			} `json:"invitation"`
		} `json:"noConnection"`
	} `json:"memberRelationship"`
}

// classify reduces the relationship union to a status + invitation state.
func (r *memberRelationshipEntity) classify() (status, invitation string) {
	mr := r.MemberRelationship
	switch {
	case jsonSet(mr.Self):
		return "self", ""
	case jsonSet(mr.Connection):
		return "connected", ""
	case mr.NoConnection != nil:
		inv := mr.NoConnection.Invitation
		switch {
		case jsonSet(inv.Invitation):
			return "not_connected", "pending"
		case jsonSet(inv.NoInvitation):
			return "not_connected", "none"
		}
		return "not_connected", ""
	}
	return "", ""
}

// NOTE: the response also ships a PrivacySettings entity, but its URN is
// fsd_privacySettings:singleton — the VIEWER's (fetching account's) own
// settings, not the fetched profile's. Deliberately not extracted.

// jsonSet reports whether a raw JSON value is present and not null —
// json.RawMessage holds the bytes "null" for explicit JSON nulls.
func jsonSet(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null"
}

// pickLocale prefers primaryLocale, falls back to the first supportedLocale.
func pickLocale(prof *entity) string {
	if prof.PrimaryLocale.Language != "" {
		return prof.PrimaryLocale.String()
	}
	if len(prof.SupportedLocales) > 0 {
		return prof.SupportedLocales[0].String()
	}
	return ""
}

// findMemberDistance walks arbitrary relationship JSON for the first
// "memberDistance" string — the key sits under both `connection` and
// `noConnection` shapes, and we only have live samples of the latter.
func findMemberDistance(raw json.RawMessage) string {
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return ""
	}
	var found string
	var walk func(v any) bool
	walk = func(v any) bool {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				if k == "memberDistance" {
					if s, ok := x.(string); ok {
						found = s
						return true
					}
				}
				if walk(x) {
					return true
				}
			}
		case []any:
			for _, x := range t {
				if walk(x) {
					return true
				}
			}
		}
		return false
	}
	walk(root)
	return found
}

// GetTopcard fetches name/headline/location/photo for a profile vanity name.
// Verified working with plain HTTP GET (probe_voyager_topcard.json).
func (c *Client) GetTopcard(vanity string) (*Topcard, error) {
	// NOTE: parentheses stay UNENCODED — exactly how LinkedIn's own frontend calls it.
	url := "https://www.linkedin.com/voyager/api/graphql" +
		"?includeWebMetadata=true" +
		"&variables=(vanityName:" + vanity + ")" +
		"&queryId=" + TopcardQueryID

	req, err := c.newRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-restli-protocol-version", "2.0.0")
	req.Header.Set("accept", "application/vnd.linkedin.normalized+json+2.1")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("voyager request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("voyager status %d: %.300s", resp.StatusCode, body)
	}

	// DEBUG: show the raw Voyager envelope exactly as LinkedIn sent it.
	// log.Printf("linkedin: voyager topcard RAW response body for %s (%d bytes):\n%s", vanity, len(body), body)

	return parseTopcard(vanity, body)
}

// parseTopcard turns a raw Voyager GraphQL body into a Topcard. Split from
// GetTopcard so captured bodies (tests, debug dumps) run the exact same
// extraction path as live fetches.
func parseTopcard(vanity string, body []byte) (*Topcard, error) {
	var vr voyagerResponse
	if err := json.Unmarshal(body, &vr); err != nil {
		return nil, fmt.Errorf("voyager JSON: %w", err)
	}

	// // DEBUG: show how the envelope looks after unmarshal — note the struct
	// // only keeps `included`, so data/meta from the envelope are dropped here.
	// log.Printf("linkedin: voyager topcard UNMARSHALLED vr for %s: %d included entities", vanity, len(vr.Included))
	// for i, raw := range vr.Included {
	// 	log.Printf("linkedin: vr.Included[%d]: %s", i, raw)
	// }

	// Index entities by URN for reference resolution. rawByURN keeps the
	// unparsed bytes so the relationship entity can be re-parsed into its
	// own typed struct (the generic entity shape doesn't fit it).
	byURN := map[string]entity{}
	rawByURN := map[string]json.RawMessage{}
	var profiles []entity
	for _, raw := range vr.Included {
		var e entity
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		if e.EntityURN != "" {
			byURN[e.EntityURN] = e
			rawByURN[e.EntityURN] = raw
		}
		if e.FirstName != "" {
			profiles = append(profiles, e)
		}
	}

	// Pick OUR profile (the one matching the vanity), not a recommendation.
	var prof *entity
	for i := range profiles {
		if profiles[i].PublicIdentifier == vanity {
			prof = &profiles[i]
			break
		}
	}
	if prof == nil && len(profiles) > 0 {
		prof = &profiles[0]
	}
	if prof == nil {
		return nil, fmt.Errorf("no profile entity found for %q", vanity)
	}

	tc := &Topcard{
		FirstName:        prof.FirstName,
		LastName:         prof.LastName,
		Headline:         prof.Headline,
		PublicIdentifier: prof.PublicIdentifier,
		ProfileURN:       prof.EntityURN,
		MemberURN:        prof.ObjectURN,
		Premium:          prof.Premium,
		Creator:          prof.Creator,
		Influencer:       prof.Influencer,
		Locale:           pickLocale(prof),
	}
	if prof.Created > 0 {
		tc.CreatedAt = time.UnixMilli(prof.Created).UTC().Format("2006-01-02")
	}
	// entityUrn looks like urn:li:fsd_profile:<id> — the <id> is vieweeProfileId for RSC calls.
	for i := len(prof.EntityURN) - 1; i >= 0; i-- {
		if prof.EntityURN[i] == ':' {
			tc.VieweeID = prof.EntityURN[i+1:]
			break
		}
	}

	// Resolve location: profile.geoLocation -> {"*geo": "urn:li:fsd_geo:X"} -> Geo entity.
	var geoRef map[string]json.RawMessage
	if json.Unmarshal(prof.GeoLocation, &geoRef) == nil {
		if raw, ok := geoRef["*geo"]; ok {
			var urn string
			if json.Unmarshal(raw, &urn) == nil {
				if g, ok := byURN[urn]; ok {
					tc.Location = g.DefaultLocalizedName
					tc.CountryISO = g.CountryISOCode
					// One more hop: location Geo -> country Geo.
					if c, ok := byURN[g.CountryURN]; ok {
						tc.Country = c.DefaultLocalizedName
					}
				}
			}
		}
	}

	// Photos: LinkedIn ships images DECOMPOSED — a rootUrl plus per-size
	// path segments (artifacts). Rebuild full URLs for every size. Profile
	// picture and cover stay SEPARATE (they're different things to consumers).
	tc.PhotoURLs = extractVectorImageURLs(prof.ProfilePicture)
	tc.CoverURLs = extractVectorImageURLs(prof.BackgroundPicture)
	// Alt text + AI flag ride alongside the profilePicture image tree.
	var photoMeta struct {
		A11yText                  string `json:"a11yText"`
		IsGeneratedOrModifiedByAI bool   `json:"isGeneratedOrModifiedByAi"`
	}
	if json.Unmarshal(prof.ProfilePicture, &photoMeta) == nil {
		tc.PhotoAltText = photoMeta.A11yText
		tc.PhotoAIGenerated = photoMeta.IsGeneratedOrModifiedByAI
	}

	// Relationship: typed re-parse of the entity the profile points at
	// via its *memberRelationship ref.
	if raw, ok := rawByURN[prof.MemberRelationshipURN]; ok {
		var rel memberRelationshipEntity
		if json.Unmarshal(raw, &rel) == nil {
			tc.RelationshipStatus, tc.InvitationStatus = rel.classify()
			tc.NetworkDistance = findMemberDistance(raw)
		}
	}
	return tc, nil
}

// extractVectorImageURLs walks arbitrary JSON, finds every VectorImage-ish
// object ({"rootUrl": ..., "artifacts": [{fileIdentifyingUrlPathSegment}]}),
// and rebuilds complete image URLs (rootUrl + segment) for all sizes.
func extractVectorImageURLs(raw json.RawMessage) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if root, ok := t["rootUrl"].(string); ok {
				if arts, ok := t["artifacts"].([]any); ok {
					for _, a := range arts {
						if am, ok := a.(map[string]any); ok {
							if seg, ok := am["fileIdentifyingUrlPathSegment"].(string); ok && seg != "" {
								full := root + seg
								if !seen[full] {
									seen[full] = true
									out = append(out, full)
								}
							}
						}
					}
				}
			}
			for _, x := range t {
				walk(x)
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	var root any
	if json.Unmarshal(raw, &root) == nil {
		walk(root)
	}
	return out
}
