package linkedin

import (
	"regexp"
	"strings"
)

// ---------- Our public schema (the 10 challenge fields live in Profile) ----------

type Experience struct {
	Title          string `json:"title"`
	Company        string `json:"company"`
	EmploymentType string `json:"employment_type,omitempty"`
	DateRange      string `json:"date_range,omitempty"`
	From           string `json:"from,omitempty"`
	To             string `json:"to,omitempty"`
	Location       string `json:"location,omitempty"`
}

type Education struct {
	School    string `json:"school"`
	Degree    string `json:"degree,omitempty"`
	DateRange string `json:"date_range,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
}

type Certification struct {
	Title      string `json:"title"`
	Issuer     string `json:"issuer,omitempty"`
	IssuedDate string `json:"issued_date,omitempty"`
}

// ---------- text-stream parsing (PR #298 philosophy: parse what a human reads) ----------

var (
	reYear    = regexp.MustCompile(`\b(19|20)\d{2}\b`)
	reIssued  = regexp.MustCompile(`^Issued\s+`)
	// word boundaries matter: "Associated with X" must NOT match "associate".
	reDegree  = regexp.MustCompile(`(?i)\b(bachelor|master|doctor|phd|mba|b\.?tech|m\.?tech|degree|diploma|certificate|associate)\b`)
	reDivider = regexp.MustCompile(`\s[·•]\s`)
	// duration-only lines ("1 yr", "2 yrs 3 mos") mark grouped-company headers.
	reDurationOnly = regexp.MustCompile(`^\d+\s+(yrs?|mos?)\b`)
)

// employmentTypes — when the org line holds ONLY one of these, the role is
// grouped under a company header above (LinkedIn groups same-company roles).
var employmentTypes = map[string]bool{
	"full-time": true, "part-time": true, "contract": true, "internship": true,
	"self-employed": true, "freelance": true, "apprenticeship": true,
	"seasonal": true, "temporary": true, "volunteer": true,
}

// noiseLeaves are UI strings, not data.
var noiseLeaves = map[string]bool{
	"Collapsed": true, "Expanded": true, "Following": true, "Follow": true,
	"Join": true, "Joined": true, "Requested": true, "Unsubscribed": true,
	"Subscribed": true, "Received": true, "Given": true, "Link": true,
	"Show more": true, "See more": true, "Show all": true,
}

func isNoise(s string) bool { return noiseLeaves[s] }

func looksLikeDates(s string) bool {
	return reYear.MatchString(s) || strings.Contains(s, "Present")
}

// splitOrgLine splits "Company · Full-time" -> company, employment type.
func splitOrgLine(s string) (string, string) {
	parts := reDivider.Split(s, 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(s), ""
}

// splitDates splits "May 2022 - Dec 2023 · 1 yr 8 mos" -> range, from, to.
func splitDates(s string) (dateRange, from, to string) {
	parts := reDivider.Split(s, 2)
	rangePart := strings.TrimSpace(parts[0])
	for _, sep := range []string{" – ", " - ", "—"} {
		if i := strings.Index(rangePart, sep); i >= 0 {
			return rangePart, strings.TrimSpace(rangePart[:i]), strings.TrimSpace(rangePart[i+len(sep):])
		}
	}
	return rangePart, rangePart, ""
}

// ParseExperienceTurns parses an Experience-section text stream into entries.
// Pattern per entry: [title, org(·type), dates, (optional) location].
func ParseExperience(leaves []string) []Experience {
	var out []Experience
	// date-line indices
	var dateIdx []int
	for i, l := range leaves {
		if looksLikeDates(l) && (strings.Contains(l, "-") || strings.Contains(l, "–") || strings.Contains(l, "Present")) {
			dateIdx = append(dateIdx, i)
		}
	}
	// Pre-scan group-company headers: a company line followed by a
	// duration-only line ("Teqtive Solutions" + "1 yr"). Grouped roles
	// (org line = employment type only) inherit the nearest header above.
	var groupIdx []int
	for i := 0; i+1 < len(leaves); i++ {
		if reDurationOnly.MatchString(leaves[i+1]) && !isNoise(leaves[i]) && !looksLikeDates(leaves[i]) {
			groupIdx = append(groupIdx, i)
		}
	}

	for k, di := range dateIdx {
		if di < 2 {
			continue
		}
		title := leaves[di-2]
		org := leaves[di-1]
		if isNoise(title) || isNoise(org) || looksLikeDates(title) {
			continue
		}
		company, empType := splitOrgLine(org)
		if empType == "" && employmentTypes[strings.ToLower(company)] {
			// grouped role: org line was ONLY the employment type
			empType = company
			company = ""
			for _, gi := range groupIdx {
				if gi < di-2 {
					company = leaves[gi]
				}
			}
		}
		dr, from, to := splitDates(leaves[di])
		exp := Experience{Title: title, Company: company, EmploymentType: empType,
			DateRange: dr, From: from, To: to}
		// location = line after dates, only if the next entry (if any) starts 2 lines later
		if di+1 < len(leaves) {
			cand := leaves[di+1]
			nextStart := len(leaves)
			if k+1 < len(dateIdx) {
				nextStart = dateIdx[k+1] - 2
			}
			if di+1 < nextStart && !isNoise(cand) && !looksLikeDates(cand) {
				loc, _ := splitOrgLine(cand) // "Orlando, Florida · Hybrid" -> keep place part
				exp.Location = loc
			}
		}
		out = append(out, exp)
	}
	return out
}

// ParseCertifications parses [title, issuer, "Issued <date>"] triples.
// "Issued by X" lines belong to honors/awards — skipped (different section).
func ParseCertifications(leaves []string) []Certification {
	var out []Certification
	for i, l := range leaves {
		if strings.HasPrefix(l, "Issued by") {
			continue
		}
		if reIssued.MatchString(l) && i >= 2 {
			out = append(out, Certification{
				Title:      leaves[i-2],
				Issuer:     issuerFromLine(leaves[i-1]),
				IssuedDate: strings.TrimSpace(reIssued.ReplaceAllString(l, "")),
			})
		}
	}
	return out
}

// issuerFromLine handles "Scrum Alliance" or "Issued by North Highland · Dec 2018" shapes.
func issuerFromLine(s string) string {
	s = strings.TrimPrefix(s, "Issued by ")
	if parts := reDivider.Split(s, 2); len(parts) == 2 {
		return strings.TrimSpace(parts[0])
	}
	return s
}

// ParseEducation parses [school, degree, dates] triples (degree line filtered by keywords).
func ParseEducation(leaves []string) []Education {
	var out []Education
	for i := 0; i+2 < len(leaves); i++ {
		school, degree, dates := leaves[i], leaves[i+1], leaves[i+2]
		if isNoise(school) || !reDegree.MatchString(degree) || !looksLikeDates(dates) {
			continue
		}
		// guards against recommendation-card lines leaking in
		if strings.HasPrefix(school, "·") || strings.Contains(school+degree+dates, "worked with") ||
			strings.Contains(school+degree+dates, "reported to") || strings.Contains(school+degree+dates, "managed ") {
			continue
		}
		dr, from, to := splitDates(dates)
		out = append(out, Education{School: school, Degree: degree, DateRange: dr, From: from, To: to})
		i += 2
	}
	return out
}

// knownLanguages — quick dictionary filter for the Languages section
// (org-membership lines sneak into that section; they don't match this list).
var knownLanguages = map[string]bool{
	"English": true, "Spanish": true, "French": true, "German": true, "Hindi": true,
	"Gujarati": true, "Marathi": true, "Punjabi": true, "Tamil": true, "Telugu": true,
	"Bengali": true, "Urdu": true, "Arabic": true, "Chinese": true, "Mandarin": true,
	"Japanese": true, "Korean": true, "Portuguese": true, "Russian": true, "Italian": true,
	"Dutch": true, "Turkish": true, "Polish": true, "Vietnamese": true, "Thai": true,
	"Indonesian": true, "Malay": true, "Persian": true, "Farsi": true, "Hebrew": true,
	"Swedish": true, "Norwegian": true, "Danish": true, "Finnish": true, "Greek": true,
}

// ParseLanguages filters a section stream down to language names.
func ParseLanguages(leaves []string) []string {
	var out []string
	for _, l := range leaves {
		if knownLanguages[l] {
			out = append(out, l)
		}
	}
	return out
}

// ParseSkills returns plausible skill names from a skills-section stream.
func ParseSkills(leaves []string) []string {
	var out []string
	for _, l := range leaves {
		if isNoise(l) || strings.HasPrefix(l, "interest_") || strings.Contains(l, "EditButton") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// ParseTopSkillsLine splits "Agile Methodologies • Management • Communication".
func ParseTopSkillsLine(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "•") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ParseAboveActivity extracts the About text (paragraphs between the "About"
// header and the next card boundary) and the "Top skills" line.
func ParseAboveActivity(leaves []string) (about string, topSkills []string) {
	var aboutParts []string
	inAbout := false
	for i, l := range leaves {
		switch l {
		case "About":
			inAbout = true
			continue
		case "Top skills", "Services", "Featured":
			inAbout = false
			if l == "Top skills" && i+1 < len(leaves) {
				topSkills = ParseTopSkillsLine(leaves[i+1])
			}
		}
		if inAbout && !isNoise(l) {
			aboutParts = append(aboutParts, l)
		}
	}
	return strings.Join(aboutParts, "\n\n"), topSkills
}

// ClassifyBelowActivityPart routes a BelowActivity PartN stream by CONTENT
// (part numbering varies per profile — never trust the number):
//
//	certifications: only when "Issued <date>" lines exist ("Issued by X" = honors)
//	education:      only when an "Education" header leaf is present
//	languages:      dictionary filter (works headerless — e.g. a lone "French")
//	skills:         small parts of short skill-ish lines
func ClassifyBelowActivityPart(leaves []string, prof *Profile, addSkills func([]string)) {
	hasIssuedLine := false
	for _, l := range leaves {
		if strings.HasPrefix(l, "Issued ") && !strings.HasPrefix(l, "Issued by") {
			hasIssuedLine = true
			break
		}
	}
	switch {
	case hasIssuedLine || containsLeaf(leaves, "Education"):
		if hasIssuedLine {
			prof.Certifications = append(prof.Certifications, ParseCertifications(leaves)...)
		}
		if containsLeaf(leaves, "Education") {
			prof.Education = append(prof.Education, ParseEducation(leaves)...)
		}
	case looksLikeSkillsPart(leaves):
		addSkills(ParseSkills(leaves))
	}
	// headerless languages can hide in any part
	if langs := ParseLanguages(leaves); len(langs) > 0 {
		prof.Languages = append(prof.Languages, langs...)
	}
}

func containsLeaf(leaves []string, word string) bool {
	for _, l := range leaves {
		if l == word {
			return true
		}
	}
	return false
}

// looksLikeSkillsPart: small parts dominated by short skill-ish lines
// (e.g. "Communication", "Way of Working (WoW)", "DASM").
func looksLikeSkillsPart(leaves []string) bool {
	if len(leaves) == 0 || len(leaves) > 12 {
		return false
	}
	for _, l := range leaves {
		if strings.Contains(l, " · ") || strings.Contains(l, "Present") || len(l) > 60 {
			return false
		}
	}
	return true
}
