package linkedin

// Profile is our public API response schema (the challenge's 10 fields,
// plus the enrichment fields added in round 4: recommendations, contact
// info, language proficiencies).
type Profile struct {
	Name            string           `json:"name"`
	Headline        string           `json:"headline"`
	Location        string           `json:"location"`
	About           string           `json:"about,omitempty"`
	Experience      []Experience     `json:"experience"`
	Education       []Education      `json:"education"`
	Skills          []string         `json:"skills"`
	Certifications  []Certification  `json:"certifications"`
	Languages       []Language       `json:"languages"`
	Recommendations []Recommendation `json:"recommendations"`
	ContactInfo     *ContactInfo     `json:"contact_info,omitempty"`
	ProfileImages   []string         `json:"profile_images"`
	LinkedInURL     string           `json:"linkedin_url"`
}
