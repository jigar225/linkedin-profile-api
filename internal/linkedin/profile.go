package linkedin

// ---------- Section schema (filled from voyager dash entities + the
// recommendations endpoint — never from render trees) ----------

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

// Language pairs a language name with its proficiency.
type Language struct {
	Name        string `json:"name"`
	Proficiency string `json:"proficiency,omitempty"`
}

// Recommendation is one recommendation entry. CURRENTLY UNPOPULATED: the
// voyager recos endpoint is a session-kill suspect and the RSC stream
// source was deleted with the cover machinery (both recoverable via git).
// The field stays in the schema (always []) so consumers don't break.
type Recommendation struct {
	Recommender  string `json:"recommender,omitempty"`
	Headline     string `json:"headline,omitempty"`
	Relationship string `json:"relationship,omitempty"`
	Date         string `json:"date,omitempty"`
	Text         string `json:"text"`
	Direction    string `json:"direction,omitempty"`
}

// Profile is our public API response schema (the challenge's 10 fields,
// plus enrichment: recommendations, contact info, language proficiencies,
// and the Voyager topcard identity block — URNs, badges/flags, locale,
// relationship and privacy settings).
type Profile struct {
	Name       string `json:"name"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Headline   string `json:"headline"`
	Location   string `json:"location"`
	Country    string `json:"country,omitempty"`
	CountryISO string `json:"country_iso,omitempty"`

	PublicIdentifier string `json:"public_identifier,omitempty"`
	ProfileURN       string `json:"profile_urn,omitempty"`
	MemberURN        string `json:"member_urn,omitempty"`
	Premium          bool   `json:"premium"`
	Creator          bool   `json:"creator"`
	Influencer       bool   `json:"influencer"`
	ProfileCreatedAt string `json:"profile_created_at,omitempty"` // ISO date
	Locale           string `json:"locale,omitempty"`

	About           string           `json:"about,omitempty"`
	Experience      []Experience     `json:"experience"`
	Education       []Education      `json:"education"`
	Skills          []string         `json:"skills"`
	Certifications  []Certification  `json:"certifications"`
	Languages       []Language       `json:"languages"`
	Recommendations []Recommendation `json:"recommendations"`
	ContactInfo     *ContactInfo     `json:"contact_info,omitempty"`

	ProfileImages           []string `json:"profile_images"`
	CoverImages             []string `json:"cover_images,omitempty"`
	ProfileImageAlt         string   `json:"profile_image_alt,omitempty"`
	ProfileImageAIGenerated bool     `json:"profile_image_ai_generated"`

	RelationshipStatus string `json:"relationship_status,omitempty"`
	NetworkDistance    string `json:"network_distance,omitempty"`
	InvitationStatus   string `json:"invitation_status,omitempty"`

	LinkedInURL string `json:"linkedin_url"`
}
