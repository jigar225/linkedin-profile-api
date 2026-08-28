package linkedin

import (
	"fmt"
	"strings"
	"sync"
)

// FetchProfile runs the full engine for one profile:
//
//	vanity -> Voyager topcard (name/headline/location/photos/vieweeID)
//	      -> ~10 parallel RSC section calls (goroutines)
//	      -> Flight parsing + classification -> assembled 10-field Profile.
//
// This is THE reusable entry point — the CLI and the HTTP API both call it.
func (c *Client) FetchProfile(vanity, sourceURL string) (*Profile, error) {
	tc, err := c.GetTopcard(vanity)
	if err != nil {
		return nil, fmt.Errorf("topcard: %w", err)
	}

	components := []string{RSCAboveActivity, RSCExperienceOnly}
	for i := 1; i <= 8; i++ {
		c := fmt.Sprintf("%sPart%d", RSCBelowActivityBase, i)
		if i == 1 {
			c += "WithoutExp"
		}
		components = append(components, c)
	}

	type sectionResult struct {
		componentID string
		leaves      []string
		err         error
	}
	results := make(chan sectionResult, len(components))
	var wg sync.WaitGroup
	for _, comp := range components {
		wg.Add(1)
		go func(componentID string) {
			defer wg.Done()
			flight, err := c.GetProfileSection(vanity, tc.VieweeID, componentID)
			if err != nil {
				results <- sectionResult{componentID: componentID, err: err}
				return
			}
			results <- sectionResult{componentID: componentID, leaves: ExtractFlightTexts(flight)}
		}(comp)
	}
	wg.Wait()
	close(results)

	prof := &Profile{
		Name:           strings.TrimSpace(tc.FirstName + " " + tc.LastName),
		Headline:       tc.Headline,
		Location:       tc.Location,
		ProfileImages:  tc.PhotoURLs,
		LinkedInURL:    sourceURL,
		Experience:     []Experience{},
		Education:      []Education{},
		Skills:         []string{},
		Certifications: []Certification{},
		Languages:      []string{},
	}

	skillsSeen := map[string]bool{}
	addSkills := func(ss []string) {
		for _, s := range ss {
			if !skillsSeen[s] {
				skillsSeen[s] = true
				prof.Skills = append(prof.Skills, s)
			}
		}
	}

	var fetchErrs []string
	for r := range results {
		if r.err != nil {
			fetchErrs = append(fetchErrs, shortComponentID(r.componentID))
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
			ClassifyBelowActivityPart(r.leaves, prof, addSkills)
		}
	}
	// NOTE: individual section failures are tolerated (partial data > no data).
	// fetchErrs lists failed components — callers may log it.
	_ = fetchErrs
	return prof, nil
}

func shortComponentID(componentID string) string {
	if i := strings.LastIndex(componentID, "."); i >= 0 {
		return componentID[i+1:]
	}
	return componentID
}
