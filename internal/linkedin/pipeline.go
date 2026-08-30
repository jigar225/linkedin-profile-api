package linkedin

import (
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
//	         skills/certs/languages) via the WEB frontend's OWN decoration
//	         (FullProfileWithEntities-93): typed data, layout-independent
//	      -> Voyager recommendations (received list)
//	      -> contact overlay (the last RSC call standing — the legacy
//	         voyager contact endpoint is 410 Gone)
//	      -> assembled Profile.
//
// This is THE reusable entry point — the CLI and the HTTP API both call it.
func (c *Client) FetchProfile(vanity, sourceURL string) (*Profile, error) {
	tc, err := c.GetTopcard(vanity)
	if err != nil {
		return nil, fmt.Errorf("topcard: %w", err)
	}

	// Human-ish rhythm between calls: 4 back-to-back machine-gun requests
	// are a bot tell. (~1s each hop; LINKEDIN_PACING=fast skips for dev.)
	sleepJitter(0.7, 1.6)

	// Dash entities = the section source of truth. A failure here leaves the
	// sections empty and validateProfile fails LOUDLY below — never silently.
	var dp *dashProfile
	if raw, err := c.GetDashProfile(vanity); err != nil {
		log.Printf("linkedin: dash profile for %s: %v", vanity, err)
	} else if parsed, err := parseDashProfile(raw); err != nil {
		log.Printf("linkedin: dash parse for %s: %v", vanity, err)
	} else {
		dp = parsed
	}

	sleepJitter(0.7, 1.6)

	// Recommendations ride their own voyager REST endpoint (received list).
	// Optional: failure must never sink the profile.
	var recos []Recommendation
	if rs, err := c.GetRecommendations(vanity); err != nil {
		log.Printf("linkedin: recommendations for %s: %v", vanity, err)
	} else {
		recos = rs
	}

	sleepJitter(0.7, 1.6)

	// Contact info = the last RSC call standing. Optional, same rule.
	var contact *ContactInfo
	if flight, err := c.GetContactInfo(vanity, tc.FirstName, tc.LastName); err != nil {
		log.Printf("linkedin: contact info for %s: %v", vanity, err)
	} else {
		contact = ParseContactInfo(ExtractFlightTexts(flight))
	}

	prof := assembleProfile(sourceURL, tc, contact)
	if dp != nil {
		applyDashProfile(prof, dp)
	}
	if len(recos) > 0 {
		prof.Recommendations = recos
	}

	if err := validateProfile(prof); err != nil {
		return nil, err
	}
	return prof, nil
}

// assembleProfile builds the Profile skeleton from the topcard + contact
// overlay. Sections get overlaid afterwards (dash entities + voyager recos).
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
