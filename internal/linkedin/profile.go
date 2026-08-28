package linkedin

// Profile is our public API response schema (the challenge's 10 fields).
type Profile struct {
	Name           string          `json:"name"`
	Headline       string          `json:"headline"`
	Location       string          `json:"location"`
	About          string          `json:"about,omitempty"`
	Experience     []Experience    `json:"experience"`
	Education      []Education     `json:"education"`
	Skills         []string        `json:"skills"`
	Certifications []Certification `json:"certifications"`
	Languages      []string        `json:"languages"`
	ProfileImages  []string        `json:"profile_images"`
	LinkedInURL    string          `json:"linkedin_url"`
}
