package linkedin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// dash.go — parsing for the voyager DASH REST endpoint
// (/voyager/api/identity/dash/profiles, decoration FullProfileWithEntities-93 —
// the WEB frontend's OWN full-profile call, captured from real traffic).
// One GET returns the whole profile as TYPED entities with structured dates —
// the layout-independent replacement for the retired RSC leaf-rule parsers.
//
// Response shape (normalized, same family as the topcard GraphQL):
//
//	{"data": {…}, "included": [ …entities… ]}
//
// Entities live flat in included[], typed by $type and cross-referenced by
// URN: the Profile entity's *profileXxx fields point at collection entities
// whose *elements arrays carry the section's entity URNs IN ORDER.
// Page 1 caps sections at 20 elements (paging.total can exceed that).

// dashProfile is the parsed intermediate: final-schema sections, ready to overlay.
type dashProfile struct {
	Summary        string
	Experience     []Experience
	Education      []Education
	Skills         []string
	Certifications []Certification
	Languages      []Language
}

// dashDate is a structured date: {year, month?} — no string parsing, ever.
type dashDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
}

// String formats as "Nov 2018", or "2015" when month is absent.
func (d dashDate) String() string {
	if d.Year == 0 {
		return ""
	}
	if d.Month < 1 || d.Month > 12 {
		return strconv.Itoa(d.Year)
	}
	return fmt.Sprintf("%s %d", time.Month(d.Month).String()[:3], d.Year)
}

type dashDateRange struct {
	Start dashDate  `json:"start"`
	End   *dashDate `json:"end"`
}

// parts returns (dateRange, from, to). presentIfOpen: an open-ended range
// reads "… - Present" (positions); education ranges stay bare.
func (dr dashDateRange) parts(presentIfOpen bool) (dateRange, from, to string) {
	from = dr.Start.String()
	if dr.End != nil {
		to = dr.End.String()
	} else if presentIfOpen && from != "" {
		to = "Present"
	}
	switch {
	case from != "" && to != "":
		dateRange = from + " - " + to
	default:
		dateRange = from
	}
	return dateRange, from, to
}

// dashProficiencies maps LinkedIn's proficiency enums to display strings.
var dashProficiencies = map[string]string{
	"ELEMENTARY":           "Elementary proficiency",
	"LIMITED_WORKING":      "Limited working proficiency",
	"PROFESSIONAL_WORKING": "Professional working proficiency",
	"FULL_PROFESSIONAL":    "Full professional proficiency",
	"NATIVE_OR_BILINGUAL":  "Native or bilingual proficiency",
}

// parseDashProfile decodes the normalized envelope and resolves the section
// entity graph: Profile -> *profileXxx collection refs -> ordered entity URNs.
func parseDashProfile(body []byte) (*dashProfile, error) {
	var env struct {
		Included []json.RawMessage `json:"included"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("dash JSON: %w", err)
	}

	byURN := map[string]json.RawMessage{}
	var profileRaw json.RawMessage
	for _, raw := range env.Included {
		var hdr struct {
			Type      string `json:"$type"`
			EntityURN string `json:"entityUrn"`
		}
		if json.Unmarshal(raw, &hdr) != nil {
			continue
		}
		if hdr.EntityURN != "" {
			byURN[hdr.EntityURN] = raw
		}
		if strings.HasSuffix(hdr.Type, ".Profile") {
			profileRaw = raw
		}
	}
	if profileRaw == nil {
		return nil, fmt.Errorf("dash: no Profile entity in included[]")
	}

	var p struct {
		Summary           string `json:"summary"`
		PositionGroupsRef string `json:"*profilePositionGroups"`
		EducationsRef     string `json:"*profileEducations"`
		SkillsRef         string `json:"*profileSkills"`
		CertificationsRef string `json:"*profileCertifications"`
		LanguagesRef      string `json:"*profileLanguages"`
	}
	if err := json.Unmarshal(profileRaw, &p); err != nil {
		return nil, fmt.Errorf("dash Profile entity: %w", err)
	}

	return &dashProfile{
		Summary:        strings.TrimSpace(p.Summary),
		Experience:     dashExperiences(byURN, p.PositionGroupsRef),
		Education:      dashEducations(byURN, p.EducationsRef),
		Skills:         dashSkills(byURN, p.SkillsRef),
		Certifications: dashCertifications(byURN, p.CertificationsRef),
		Languages:      dashLanguages(byURN, p.LanguagesRef),
	}, nil
}

// applyDashProfile overlays dash-entity sections onto prof, REPLACING each
// section ONLY when dash actually carries it — otherwise whatever is there
// stays (per-section fallback, never all-or-nothing). Recommendations are
// untouched: dash doesn't ship them.
func applyDashProfile(prof *Profile, dp *dashProfile) {
	if dp.Summary != "" {
		prof.About = dp.Summary
	}
	if len(dp.Experience) > 0 {
		prof.Experience = dp.Experience
	}
	if len(dp.Education) > 0 {
		prof.Education = dp.Education
	}
	if len(dp.Skills) > 0 {
		prof.Skills = dp.Skills
	}
	if len(dp.Certifications) > 0 {
		prof.Certifications = dp.Certifications
	}
	if len(dp.Languages) > 0 {
		prof.Languages = dp.Languages
	}
}

// collectionURNs resolves a section's collection entity to its ordered
// element URNs. Empty when the section is absent (member never filled it).
func collectionURNs(byURN map[string]json.RawMessage, ref string) []string {
	raw, ok := byURN[ref]
	if !ok {
		return nil
	}
	var coll struct {
		Elements []string `json:"*elements"`
	}
	if json.Unmarshal(raw, &coll) != nil {
		return nil
	}
	return coll.Elements
}

// entityName resolves a referenced entity's display name (e.g. employmentType).
func entityName(byURN map[string]json.RawMessage, ref string) string {
	raw, ok := byURN[ref]
	if !ok {
		return ""
	}
	var e struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return ""
	}
	return e.Name
}

// dashExperiences flattens position GROUPS (LinkedIn groups same-company
// roles) into one Experience per role; a role without its own company name
// inherits the group's.
func dashExperiences(byURN map[string]json.RawMessage, ref string) []Experience {
	var out []Experience
	for _, urn := range collectionURNs(byURN, ref) {
		raw, ok := byURN[urn]
		if !ok {
			continue
		}
		var g struct {
			CompanyName  string `json:"companyName"`
			PositionsRef string `json:"*profilePositionInPositionGroup"`
		}
		if json.Unmarshal(raw, &g) != nil {
			continue
		}
		for _, pURN := range collectionURNs(byURN, g.PositionsRef) {
			praw, ok := byURN[pURN]
			if !ok {
				continue
			}
			var pos struct {
				Title         string        `json:"title"`
				CompanyName   string        `json:"companyName"`
				Location      string        `json:"locationName"`
				DateRange     dashDateRange `json:"dateRange"`
				EmploymentRef string        `json:"*employmentType"`
			}
			if json.Unmarshal(praw, &pos) != nil {
				continue
			}
			company := pos.CompanyName
			if company == "" {
				company = g.CompanyName
			}
			dr, from, to := pos.DateRange.parts(true)
			out = append(out, Experience{
				Title:          pos.Title,
				Company:        company,
				EmploymentType: entityName(byURN, pos.EmploymentRef),
				DateRange:      dr,
				From:           from,
				To:             to,
				Location:       pos.Location,
			})
		}
	}
	return out
}

// dashEducations maps education entities; degree display matches LinkedIn's
// UI ("Master of Business Administration - MBA, Human Resources and Marketing").
func dashEducations(byURN map[string]json.RawMessage, ref string) []Education {
	var out []Education
	for _, urn := range collectionURNs(byURN, ref) {
		raw, ok := byURN[urn]
		if !ok {
			continue
		}
		var e struct {
			SchoolName   string        `json:"schoolName"`
			DegreeName   string        `json:"degreeName"`
			FieldOfStudy string        `json:"fieldOfStudy"`
			DateRange    dashDateRange `json:"dateRange"`
		}
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		degree := e.DegreeName
		if e.FieldOfStudy != "" {
			if degree != "" {
				degree += ", "
			}
			degree += e.FieldOfStudy
		}
		dr, from, to := e.DateRange.parts(false)
		out = append(out, Education{
			School:    e.SchoolName,
			Degree:    degree,
			DateRange: dr,
			From:      from,
			To:        to,
		})
	}
	return out
}

func dashSkills(byURN map[string]json.RawMessage, ref string) []string {
	var out []string
	for _, urn := range collectionURNs(byURN, ref) {
		raw, ok := byURN[urn]
		if !ok {
			continue
		}
		var s struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		if n := strings.TrimSpace(s.Name); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func dashCertifications(byURN map[string]json.RawMessage, ref string) []Certification {
	var out []Certification
	seen := map[string]bool{}
	for _, urn := range collectionURNs(byURN, ref) {
		raw, ok := byURN[urn]
		if !ok {
			continue
		}
		var c struct {
			Name      string        `json:"name"`
			Authority string        `json:"authority"`
			DateRange dashDateRange `json:"dateRange"` // start = issued date
		}
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		title := strings.TrimSpace(c.Name) // dash ships trailing spaces
		issuer := strings.TrimSpace(c.Authority)
		issued := c.DateRange.Start.String()
		key := title + "|" + issuer + "|" + issued
		if title == "" || seen[key] {
			continue // LinkedIn's own data ships dupes (distinct entityUrns, same cert)
		}
		seen[key] = true
		out = append(out, Certification{Title: title, Issuer: issuer, IssuedDate: issued})
	}
	return out
}

func dashLanguages(byURN map[string]json.RawMessage, ref string) []Language {
	var out []Language
	for _, urn := range collectionURNs(byURN, ref) {
		raw, ok := byURN[urn]
		if !ok {
			continue
		}
		var l struct {
			Name        string `json:"name"`
			Proficiency string `json:"proficiency"` // enum: FULL_PROFESSIONAL, …
		}
		if json.Unmarshal(raw, &l) != nil {
			continue
		}
		name := strings.TrimSpace(l.Name) // dash ships trailing spaces ("English ")
		if name == "" {
			continue
		}
		prof := dashProficiencies[l.Proficiency]
		if prof == "" {
			prof = l.Proficiency // unknown enum stays visible, never guessed
		}
		out = append(out, Language{Name: name, Proficiency: prof})
	}
	return out
}
