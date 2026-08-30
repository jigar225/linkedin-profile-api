package linkedin

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// sectionResult is one component's fetch outcome from the parallel fan-out.
type sectionResult struct {
	componentID string
	flight      string
	leaves      []string
	err         error
}

// sectionWaves replay the real SPA's lazy-load rhythm, captured from live
// browser traffic (round-4 timing analysis): the above-fold card fires
// first, Experience and the first below-activity chunk land alone seconds
// apart, and the remaining chunks fire as one scroll-triggered burst.
// Firing everything at t+0 is an automation fingerprint — LinkedIn's own
// client never does it. (Part8 is deliberately absent: the real SPA never
// requests it and it only ever 500s — a pure bot tell.)
var sectionWaves = [][]string{
	{RSCAboveActivity},
	{RSCExperienceOnly},
	{RSCBelowActivityBase + "Part1WithoutExp"},
	{
		RSCBelowActivityBase + "Part2",
		RSCBelowActivityBase + "Part3",
		RSCBelowActivityBase + "Part4",
		RSCBelowActivityBase + "Part5",
		RSCBelowActivityBase + "Part6",
		RSCBelowActivityBase + "Part7",
	},
}

// waveGaps are the [min,max] seconds of "user reading/scrolling" silence
// between waves (real rhythm: ~4.7s / ~1s / ~2s after each respective wave),
// jittered per run so the timing pattern never repeats exactly.
var waveGaps = [][2]float64{
	{3.0, 5.0}, // after AboveActivity
	{1.0, 2.0}, // after ExperienceOnly
	{1.5, 2.5}, // after Part1WithoutExp
}

// humanPacing is the default request rhythm: wave-shaped like the real SPA.
// LINKEDIN_PACING=fast restores the all-at-once burst — local dev only.
func humanPacing() bool {
	return !strings.EqualFold(os.Getenv("LINKEDIN_PACING"), "fast")
}

func sleepJitter(min, max float64) {
	time.Sleep(time.Duration((min + rand.Float64()*(max-min)) * float64(time.Second)))
}

// FetchProfile runs the full engine for one profile:
//
//	vanity -> Voyager topcard (name/headline/location/photos/vieweeID)
//	      -> 9 RSC section calls in SPA-shaped waves (one capped burst
//	         instead when LINKEDIN_PACING=fast)
//	      -> contact overlay, trailing the waves like a late human click
//	      -> Flight parsing + classification -> assembled Profile.
//
// This is THE reusable entry point — the CLI and the HTTP API both call it.
func (c *Client) FetchProfile(vanity, sourceURL string) (*Profile, error) {
	tc, err := c.GetTopcard(vanity)
	if err != nil {
		return nil, fmt.Errorf("topcard: %w", err)
	}

	human := humanPacing()
	results := make(chan sectionResult, 10)
	var wg sync.WaitGroup
	// Fast mode only: cap the burst at 3 in flight — firing everything at
	// once storms fresh TLS handshakes (observed as intermittent
	// "tls: bad record MAC" bursts). Human mode doesn't need the cap: waves
	// are <=6 wide and reuse connections warmed by the previous waves.
	sem := make(chan struct{}, 3)

	launch := func(componentID string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !human {
				sem <- struct{}{}
				defer func() { <-sem }()
			}
			flight, err := c.GetProfileSection(vanity, tc.VieweeID, componentID)
			if err != nil {
				results <- sectionResult{componentID: componentID, err: err}
				return
			}
			results <- sectionResult{componentID: componentID, flight: flight, leaves: ExtractFlightTexts(flight)}
		}()
	}

	for i, wave := range sectionWaves {
		if i > 0 && human {
			// Wave barrier + pause: the SPA lazy-loads the next chunk only
			// after the previous one rendered and the user "kept scrolling".
			wg.Wait()
			sleepJitter(waveGaps[i-1][0], waveGaps[i-1][1])
		}
		for _, comp := range wave {
			launch(comp)
		}
	}

	// Contact info rides the overlay navigation action — always fetched, but
	// timed like a human: the real SPA opens the overlay only on click, well
	// after page load, so in human pacing it trails the section waves by a
	// couple of seconds. Optional data: failure must never sink the profile.
	contactCh := make(chan *ContactInfo, 1)
	if human {
		wg.Wait()
		sleepJitter(2.0, 3.0)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !human {
			sem <- struct{}{}
			defer func() { <-sem }()
		}
		flight, err := c.GetContactInfo(vanity, tc.FirstName, tc.LastName)
		if err != nil {
			log.Printf("linkedin: contact info for %s: %v", vanity, err)
			contactCh <- nil
			return
		}
		contactCh <- ParseContactInfo(ExtractFlightTexts(flight))
	}()
	wg.Wait()
	close(results)

	var all []sectionResult
	var fetchErrs []string
	for r := range results {
		all = append(all, r)
		if r.err != nil {
			fetchErrs = append(fetchErrs, fmt.Sprintf("%s: %.120s", shortComponentID(r.componentID), r.err))
		}
	}
	prof := assembleProfile(vanity, sourceURL, tc, all, <-contactCh)

	// Optional debug dumps for parser work: raw Flight payloads + extracted
	// text leaves per component. Opt-in via LINKEDIN_DEBUG_DIR.
	if dir := os.Getenv("LINKEDIN_DEBUG_DIR"); dir != "" {
		dumpProfileStreams(dir, vanity, all)
	}

	// Individual section failures are tolerated (partial data > no data),
	// but they must be VISIBLE — silent parser drift is the real enemy.
	if len(fetchErrs) > 0 {
		total := 0
		for _, w := range sectionWaves {
			total += len(w)
		}
		log.Printf("linkedin: %d/%d section fetches failed for %s: %v",
			len(fetchErrs), total, vanity, fetchErrs)
	}

	if err := validateProfile(prof); err != nil {
		return nil, err
	}
	return prof, nil
}

// assembleProfile turns fetched section streams into the final Profile.
// Shared by FetchProfile and the offline corpus harness (corpus_test.go) —
// one assembly path, so the lab tests exactly what production runs.
func assembleProfile(vanity, sourceURL string, tc *Topcard, results []sectionResult, contact *ContactInfo) *Profile {
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

	skillsSeen := map[string]bool{}
	addSkills := func(ss []string) {
		for _, s := range ss {
			if !skillsSeen[s] {
				skillsSeen[s] = true
				prof.Skills = append(prof.Skills, s)
			}
		}
	}
	langsSeen := map[string]bool{}
	addLangs := func(ls []Language) {
		for _, l := range ls {
			if !langsSeen[l.Name] {
				langsSeen[l.Name] = true
				prof.Languages = append(prof.Languages, l)
			}
		}
	}

	for _, r := range results {
		if r.err != nil {
			continue
		}
		switch r.componentID {
		case RSCAboveActivity:
			about, topSkills := ParseAboveActivity(r.leaves)
			prof.About = about
			addSkills(topSkills)
		case RSCExperienceOnly:
			prof.Experience = ParseExperience(r.leaves)
		default:
			ClassifyBelowActivityPart(r.leaves, r.flight, prof, addSkills, addLangs, tc.FirstName)
		}
	}
	return prof
}

// dumpProfileStreams writes each component's raw Flight payload and its
// extracted text leaves to <dir>/<vanity>/<component>.{flight,leaves}.txt.
// Debug-only; failures here must not break a fetch.
func dumpProfileStreams(dir, vanity string, results []sectionResult) {
	base := filepath.Join(dir, vanity)
	if err := os.MkdirAll(base, 0755); err != nil {
		log.Printf("linkedin: debug dump mkdir: %v", err)
		return
	}
	for _, r := range results {
		if r.err != nil {
			continue
		}
		name := shortComponentID(r.componentID)
		os.WriteFile(filepath.Join(base, name+".flight.txt"), []byte(r.flight), 0644)
		os.WriteFile(filepath.Join(base, name+".leaves.txt"), []byte(strings.Join(r.leaves, "\n")), 0644)
	}
}

// validateProfile guards against silent parser drift. A real profile always
// has a name, and an empty everything (about + all sections) means the page
// structure changed under us — fail loudly instead of returning clean-looking
// junk JSON.
func validateProfile(p *Profile) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("validation: empty profile name (parser drift or auth wall)")
	}
	if p.About == "" && len(p.Experience) == 0 && len(p.Education) == 0 &&
		len(p.Skills) == 0 && len(p.Certifications) == 0 && len(p.Languages) == 0 {
		return fmt.Errorf("validation: all sections empty for %s (parser drift or auth wall)", p.Name)
	}
	return nil
}

func shortComponentID(componentID string) string {
	if i := strings.LastIndex(componentID, "."); i >= 0 {
		return componentID[i+1:]
	}
	return componentID
}
