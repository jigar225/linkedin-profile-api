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
	reDivider = regexp.MustCompile(`\s[·•]\s`)
	// duration-only lines ("1 yr", "2 yrs 3 mos") mark grouped-company headers.
	reDurationOnly = regexp.MustCompile(`^\d+\s+(yrs?|mos?)\b`)
	// endorsement counts ("21 endorsements") are skills metadata, not skills.
	reEndorsements = regexp.MustCompile(`^\d[\d,]* endorsements?$`)
	// media/document attachment leaves ("Intro_To_CyberSecurity.pdf") ride
	// inside cert/education cards — not entry data (verified: Aayush).
	reAttachment = regexp.MustCompile(`(?i)\.(pdf|png|jpe?g|gif|docx?|pptx?|xlsx?|mp4|zip)$`)
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
	// "Featured" is a card header that rides right after "About" on creator
	// layouts (headers stream before content) — it is NOT an about boundary.
	"Featured": true,
}

func isNoise(s string) bool { return noiseLeaves[s] }

func looksLikeDates(s string) bool {
	return reYear.MatchString(s) || strings.Contains(s, "Present")
}

// looksLikeLocation guards the experience-location heuristic: real geo lines
// are short ("India", "Hyderabad, Telangana, India"); company taglines and
// descriptions are long, sentence-y, or emoji-laden (verified: Shradha's
// Apna College tagline leaking in as a "location").
func looksLikeLocation(s string) bool {
	if len(s) > 40 || strings.Contains(s, ". ") {
		return false
	}
	for _, r := range s {
		if r > 0x2FFF { // emoji & friends (en_US locale = geo lines stay ASCII-ish)
			return false
		}
	}
	return true
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
			if di+1 < nextStart && !isNoise(cand) && !looksLikeDates(cand) && looksLikeLocation(cand) {
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

// ParseEducation walks an Education-section stream. An entry is: school name,
// optional detail line (degree/field/program), optional dates line. All three
// shapes exist in the wild (verified): dated+degreed (Aayush), dates-only
// (Bill Gates' Harvard dropout entry), no-dates field-of-study (Shradha).
// Collection stops at description bullets / score lines / next-section headers.
func ParseEducation(leaves []string) []Education {
	var out []Education
	i := 0
	for i < len(leaves) && leaves[i] != "Education" {
		i++
	}
	i++ // past the header (or past the end → loop exits)
	for i < len(leaves) {
		l := leaves[i]
		if isNoise(l) || reAttachment.MatchString(l) {
			i++
			continue
		}
		if isEduDescription(l) || eduSectionHeaders[l] {
			break // rest is free-text description, scores, or another section
		}
		if looksLikeDates(l) {
			i++ // orphan date line — skip
			continue
		}
		// Cert/honor triples [title, issuer, "Issued <date>"] can precede the
		// education entries in mixed chunks (verified: Aayush Part1, no
		// "Certifications" header) — skip the whole triple.
		if i+2 < len(leaves) && strings.HasPrefix(leaves[i+2], "Issued ") {
			i += 3
			continue
		}
		e := Education{School: l}
		if i+1 < len(leaves) && !isNoise(leaves[i+1]) && !reAttachment.MatchString(leaves[i+1]) &&
			!looksLikeDates(leaves[i+1]) && !isEduDescription(leaves[i+1]) && !eduSectionHeaders[leaves[i+1]] {
			e.Degree = leaves[i+1]
			i++
		}
		if i+1 < len(leaves) && looksLikeDates(leaves[i+1]) {
			e.DateRange, e.From, e.To = splitDates(leaves[i+1])
			i++
		}
		out = append(out, e)
		i++
	}
	return out
}

// isEduDescription marks the free-text tail of an education card:
// bullet lines and long sentences are descriptions, not entries.
func isEduDescription(s string) bool {
	return strings.HasPrefix(s, "- ") || len(s) > 100
}

// eduSectionHeaders terminate education parsing when chunks mix sections.
var eduSectionHeaders = map[string]bool{
	"Certifications": true, "Licenses & certifications": true,
	"Honors & awards": true, "Skills": true, "Languages": true,
	"Recommendations": true, "Interests": true, "Courses": true,
	"Projects": true, "Publications": true, "Patents": true,
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

// skillJunk marks endorsement/assessment metadata lines — they ride along
// with skill names in skills sections (verified: Shradha) but aren't skills.
func skillJunk(s string) bool {
	return strings.HasPrefix(s, "Endorsed by ") ||
		reEndorsements.MatchString(s) ||
		s == "Passed LinkedIn Skill Assessment"
}

// ParseSkills returns plausible skill names from a skills-section stream.
func ParseSkills(leaves []string) []string {
	var out []string
	for _, l := range leaves {
		if isNoise(l) || skillJunk(l) || strings.HasPrefix(l, "interest_") || strings.Contains(l, "EditButton") {
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
// header and the next REAL card boundary) and the "Top skills" line.
// Layout quirk (verified on creator profiles): card headers can stream BEFORE
// content — "About","Featured",then paragraphs — so "Featured" is noise, and
// collection stops only at "Top skills"/"Services"/"Post".
func ParseAboveActivity(leaves []string) (about string, topSkills []string) {
	var aboutParts []string
	inAbout := false
	for i, l := range leaves {
		switch l {
		case "About":
			inAbout = true
			continue
		case "Top skills", "Services", "Post":
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
// (e.g. "Communication", "Way of Working (WoW)", "DASM"). Endorsement junk
// is stripped BEFORE the size cap so big real skill lists survive.
func looksLikeSkillsPart(leaves []string) bool {
	var real []string
	for _, l := range leaves {
		if !isNoise(l) && !skillJunk(l) && !strings.HasPrefix(l, "interest_") && !strings.Contains(l, "EditButton") {
			real = append(real, l)
		}
	}
	if len(real) == 0 || len(real) > 12 {
		return false
	}
	for _, l := range real {
		if strings.Contains(l, " · ") || strings.Contains(l, "Present") || len(l) > 60 {
			return false
		}
	}
	return true
}
