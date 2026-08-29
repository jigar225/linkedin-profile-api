package linkedin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// corpusRoot points at raw captured RSC streams extracted from a network
// capture (_dev/extract_corpus.py). It lives OUTSIDE the repo on purpose:
// the streams contain real people's profile data, so the corpus never
// ships. When absent, these tests skip — `go test ./...` stays green for
// everyone.
var corpusRoot = filepath.Join("..", "..", "..", "_dev", "corpus")

// corpusComponents lists the section components in deterministic assembly
// order (the pipeline's wave order), so golden output is stable.
var corpusComponents = []string{
	RSCAboveActivity,
	RSCExperienceOnly,
	RSCBelowActivityBase + "Part1WithoutExp",
	RSCBelowActivityBase + "Part2",
	RSCBelowActivityBase + "Part3",
	RSCBelowActivityBase + "Part4",
	RSCBelowActivityBase + "Part5",
	RSCBelowActivityBase + "Part6",
	RSCBelowActivityBase + "Part7",
}

// TestCorpus runs the production parser stack (assembleProfile — the same
// function the live pipeline calls) over captured raw streams and diffs the
// assembled profile against <corpus>/<vanity>/profile.golden.json.
// A missing golden = bootstrap mode: the file is written (REVIEW IT before
// trusting it) and the run passes; later runs diff against it, so any
// parser change that alters output on real captured data fails loudly.
func TestCorpus(t *testing.T) {
	dirs, err := os.ReadDir(corpusRoot)
	if err != nil {
		t.Skipf("corpus absent (%v) — extract one with _dev/extract_corpus.py", err)
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		vanity := d.Name()
		t.Run(vanity, func(t *testing.T) {
			dir := filepath.Join(corpusRoot, vanity)
			var all []sectionResult
			for _, comp := range corpusComponents {
				raw, err := os.ReadFile(filepath.Join(dir, shortComponentID(comp)+".flight.txt"))
				if err != nil {
					continue // section never fired or body not captured
				}
				flight := string(raw)
				all = append(all, sectionResult{
					componentID: comp,
					flight:      flight,
					leaves:      ExtractFlightTexts(flight),
				})
			}
			if len(all) == 0 {
				t.Skip("no streams captured for this profile")
			}
			var contact *ContactInfo
			if raw, err := os.ReadFile(filepath.Join(dir, "contactOverlay.flight.txt")); err == nil {
				contact = ParseContactInfo(ExtractFlightTexts(string(raw)))
			}
			// Topcard is out of corpus scope: the real SPA loads profiles
			// via the unified page stream, not the vanity query, so no
			// topcard bodies exist in the capture. Name/headline/location/
			// images stay empty here — this tests SECTION parsing.
			prof := assembleProfile(vanity, "https://www.linkedin.com/in/"+vanity+"/",
				&Topcard{}, all, contact)

			got, _ := json.MarshalIndent(prof, "", "  ")
			goldenPath := filepath.Join(dir, "profile.golden.json")
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				if err := os.WriteFile(goldenPath, got, 0644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("golden bootstrapped — REVIEW IT: %s", goldenPath)
				return
			}
			if string(want) != string(got) {
				actualPath := filepath.Join(dir, "profile.actual.json")
				os.WriteFile(actualPath, got, 0644)
				t.Errorf("parser output drifted from golden:\n  diff %s %s", goldenPath, actualPath)
			}
		})
	}
}

// TestCorpusMaitreyDirection pins the pill_checked toggle-state extraction
// on the one captured profile with real recommendations: the stream renders
// the RECEIVED list (received=true, given=false).
func TestCorpusMaitreyDirection(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(corpusRoot,
		"maitrey-trivedi-theta-technolabs", "profileCardsBelowActivityPart2.flight.txt"))
	if err != nil {
		t.Skip("corpus absent")
	}
	if d := recommendationDirection(string(raw)); d != "received" {
		t.Errorf("recommendationDirection = %q, want %q", d, "received")
	}
}
