package linkedin

import (
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

// sleepJitter pauses a random beat in [min,max] seconds — the human-ish
// rhythm between upstream calls (nothing fires at machine-gun pace).
// LINKEDIN_PACING=fast skips it (local dev only).
func sleepJitter(min, max float64) {
	if strings.EqualFold(os.Getenv("LINKEDIN_PACING"), "fast") {
		return
	}
	time.Sleep(time.Duration((min + rand.Float64()*(max-min)) * float64(time.Second)))
}

// FetchProfile runs the full engine for one profile:
//
//	vanity -> Voyager topcard (name/headline/location/photos/relationship)
//	      -> Voyager DASH full-profile entities (about/experience/education/
//	         skills/certs/languages) — one call, fired like the SPA's own
//	      -> contact overlay (navigation action, trails like a late click)
//	      -> assembled Profile.
//
// Three calls total — every one a call the real web app actually makes,
// each riding the per-fetch page context (fresh tracking IDs) and the
// Chrome-impersonating transport. Session-death policy: ErrSessionExpired
// at ANY stage aborts immediately — firing the remaining calls with a dead
// session just hammers the risk engine and burns the account further.
//
// This is THE reusable entry point — the CLI and the HTTP API both call it.
func (c *Client) FetchProfile(vanity, sourceURL string) (*Profile, error) {
	// One fresh page context per profile view — the SPA mints its tracking
	// IDs per page load, and so do we (see pageContext).
	c = c.forProfile()

	tc, err := c.GetTopcard(vanity)
	if err != nil {
		return nil, fmt.Errorf("topcard: %w", err)
	}

	dp, err := c.fetchSections(vanity)
	if err != nil {
		return nil, err // session death — abort before the contact call
	}

	sleepJitter(1.0, 2.5) // the overlay opens on a human click, late

	// Contact info = navigation action. Optional: failure never sinks the profile.
	var contact *ContactInfo
	if flight, err := c.GetContactInfo(vanity, tc.FirstName, tc.LastName); err != nil {
		if errors.Is(err, ErrSessionExpired) {
			return nil, err
		}
		log.Printf("linkedin: contact info for %s: %v", vanity, err)
	} else {
		contact = ParseContactInfo(ExtractFlightTexts(flight))
	}

	prof := assembleProfile(sourceURL, tc, contact)
	if dp != nil {
		applyDashProfile(prof, dp)
	}

	if err := validateProfile(prof); err != nil {
		return nil, err
	}
	return prof, nil
}

// fetchSections is the section leg: ONE dash call after a human-ish beat.
// A dash failure leaves sections empty and validateProfile fails LOUDLY
// downstream — never silently.
func (c *Client) fetchSections(vanity string) (*dashProfile, error) {
	sleepJitter(0.7, 1.6)

	raw, err := c.GetDashProfile(vanity)
	if errors.Is(err, ErrSessionExpired) {
		return nil, err
	}
	if err != nil {
		log.Printf("linkedin: dash profile for %s: %v", vanity, err)
		return nil, nil
	}
	parsed, err := parseDashProfile(raw)
	if err != nil {
		log.Printf("linkedin: dash parse for %s: %v", vanity, err)
		return nil, nil
	}
	return parsed, nil
}

// assembleProfile builds the Profile skeleton from the topcard + contact
// overlay. Sections get overlaid afterwards (dash entities).
func assembleProfile(sourceURL string, tc *Topcard, contact *ContactInfo) *Profile {
	prof := &Profile{
		Name:                    strings.TrimSpace(tc.FirstName + " " + tc.LastName),
		FirstName:               tc.FirstName,
		LastName:                tc.LastName,
		Headline:                tc.Headline,
		Location:                tc.Location,
		Country:                 tc.Country,
		CountryISO:              tc.CountryISO,
		PublicIdentifier:        tc.PublicIdentifier,
		ProfileURN:              tc.ProfileURN,
		MemberURN:               tc.MemberURN,
		Premium:                 tc.Premium,
		Creator:                 tc.Creator,
		Influencer:              tc.Influencer,
		ProfileCreatedAt:        tc.CreatedAt,
		Locale:                  tc.Locale,
		ProfileImages:           tc.PhotoURLs,
		CoverImages:             tc.CoverURLs,
		ProfileImageAlt:         tc.PhotoAltText,
		ProfileImageAIGenerated: tc.PhotoAIGenerated,
		RelationshipStatus:      tc.RelationshipStatus,
		NetworkDistance:         tc.NetworkDistance,
		InvitationStatus:        tc.InvitationStatus,
		LinkedInURL:             sourceURL,
		Experience:              []Experience{},
		Education:               []Education{},
		Skills:                  []string{},
		Certifications:          []Certification{},
		Languages:               []Language{},
		Recommendations:         []Recommendation{},
	}
	prof.ContactInfo = contact
	return prof
}

// validateProfile guards against silent drift. A real profile always has a
// name, and an empty everything (about + all sections) means the endpoints
// changed under us — fail loudly instead of returning clean-looking junk.
func validateProfile(p *Profile) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("validation: empty profile name (parser drift or auth wall)")
	}
	if p.About == "" && len(p.Experience) == 0 && len(p.Education) == 0 &&
		len(p.Skills) == 0 && len(p.Certifications) == 0 && len(p.Languages) == 0 {
		return fmt.Errorf("validation: all sections empty for %s (endpoint drift or auth wall)", p.Name)
	}
	return nil
}
