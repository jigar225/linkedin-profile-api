package linkedin

import (
	"encoding/json"
	"fmt"
	"io"
)

// TopcardQueryID is LinkedIn's persisted-query id for profile-by-vanity-name.
// Captured from real traffic (network_log.jsonl). If it ever 400s, re-capture.
const TopcardQueryID = "voyagerIdentityDashProfiles.34ead06db82a2cc9a778fac97f69ad6a"

// Topcard is the verified "top of profile" data from Voyager GraphQL.
type Topcard struct {
	FirstName  string
	LastName   string
	Headline   string
	Location   string   // resolved from the Geo entity
	ProfileURN string   // urn:li:fsd_profile:<id> — needed as vieweeProfileId for RSC calls
	VieweeID   string   // just the <id> part
	PhotoURLs  []string // media.licdn.com URLs found in the profilePicture tree
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
	// Geo fields
	DefaultLocalizedName string `json:"defaultLocalizedName"`
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

	resp, err := c.http.Do(req)
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

	var vr voyagerResponse
	if err := json.Unmarshal(body, &vr); err != nil {
		return nil, fmt.Errorf("voyager JSON: %w", err)
	}

	// Index entities by URN for reference resolution.
	byURN := map[string]entity{}
	var profiles []entity
	for _, raw := range vr.Included {
		var e entity
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		if e.EntityURN != "" {
			byURN[e.EntityURN] = e
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
		FirstName:  prof.FirstName,
		LastName:   prof.LastName,
		Headline:   prof.Headline,
		ProfileURN: prof.EntityURN,
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
				}
			}
		}
	}

	// Photos: LinkedIn ships images DECOMPOSED — a rootUrl plus per-size
	// path segments (artifacts). Rebuild full URLs for every size.
	tc.PhotoURLs = extractVectorImageURLs(prof.ProfilePicture)
	if banner := extractVectorImageURLs(prof.BackgroundPicture); len(banner) > 0 {
		tc.PhotoURLs = append(tc.PhotoURLs, banner...) // banner included when the profile has one
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
