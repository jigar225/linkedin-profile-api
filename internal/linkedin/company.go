package linkedin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Company is our public schema for organization pages (company/school).
type Company struct {
	Type        string   `json:"type"` // "company" or "school"
	Name        string   `json:"name"`
	Tagline     string   `json:"tagline,omitempty"`
	Description string   `json:"description,omitempty"`
	Industry    string   `json:"industry,omitempty"`
	Website     string   `json:"website,omitempty"`
	Location    string   `json:"location,omitempty"`
	Founded     int      `json:"founded,omitempty"`
	StaffCount  int64    `json:"staff_count,omitempty"`
	StaffRange  string   `json:"staff_range,omitempty"`
	Followers   int64    `json:"followers,omitempty"`
	Specialties []string `json:"specialties,omitempty"`
	LogoURLs    []string `json:"logo_urls,omitempty"`
	CoverURLs   []string `json:"cover_urls,omitempty"`
	LinkedInURL string   `json:"linkedin_url"`
}

// CompanyResolveQueryID: slug -> urn resolver (light). CompanyFullQueryID:
// urn list -> full org data (fat). Two-step, exactly like profiles.
const (
	CompanyResolveQueryID = "voyagerOrganizationDashCompanies.f8854567952d792166f11b3e1483233f"
	CompanyFullQueryID    = "voyagerOrganizationDashCompanies.caea806644344ed392ac74966008183b"
)

// GetCompany fetches an organization's full data by its URL slug.
// Schools are organizations under the hood — the same queries serve /school/.
//
// Step 1: universalName (slug) -> company URN (light query)
// Step 2: company URN -> full org data (fat query)
func (c *Client) GetCompany(slug, urlType string) (*Company, error) {
	// ---- step 1: resolve slug -> urn ----
	resolveURL := voyagerGraphQLURL + "(universalName:" + slug + ")&queryId=" + CompanyResolveQueryID
	resolveBody, err := c.getVoyager(resolveURL)
	if err != nil {
		return nil, fmt.Errorf("company resolve: %w", err)
	}
	urn, name := findCompanyURN(resolveBody, slug)
	if urn == "" {
		return nil, fmt.Errorf("no organization found for slug %q", slug)
	}

	// ---- step 2: urn -> full data ----
	// NOTE: the browser percent-encodes the colons inside the urn value
	// (urn%3Ali%3Afsd_company%3A3608) — raw colons get a 400 from Voyager.
	encodedURN := strings.ReplaceAll(urn, ":", "%3A")
	fullURL := voyagerGraphQLURL + "(companyUrns:List(" + encodedURN + "))&queryId=" + CompanyFullQueryID
	fullBody, err := c.getVoyager(fullURL)
	if err != nil {
		return nil, fmt.Errorf("company full: %w", err)
	}

	// dev evidence: dump the raw fat body for parser tightening.
	// Opt-in via LINKEDIN_DEBUG_DIR — production runs must not touch disk.
	if dir := os.Getenv("LINKEDIN_DEBUG_DIR"); dir != "" {
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "company_full_raw.json"), fullBody, 0644)
	}

	co := &Company{
		Type:        urlType,
		Name:        name,
		LinkedInURL: "https://www.linkedin.com/" + urlType + "/" + slug + "/",
	}
	parseCompanyFull(fullBody, co)
	return co, nil
}

const voyagerGraphQLURL = "https://www.linkedin.com/voyager/api/graphql?includeWebMetadata=true&variables="

// getVoyager fires an authenticated Voyager GraphQL GET and returns the body.
func (c *Client) getVoyager(url string) ([]byte, error) {
	req, err := c.newRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-restli-protocol-version", "2.0.0")
	req.Header.Set("accept", "application/vnd.linkedin.normalized+json+2.1")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("voyager status %d: %.300s", resp.StatusCode, body)
	}
	return body, nil
}

// findCompanyURN pulls the org entity's urn + name out of the resolve response.
func findCompanyURN(body []byte, slug string) (urn, name string) {
	var vr voyagerResponse
	if json.Unmarshal(body, &vr) != nil {
		return "", ""
	}
	for _, raw := range vr.Included {
		var e struct {
			EntityURN    string `json:"entityUrn"`
			Name         string `json:"name"`
			UniversalName string `json:"universalName"`
		}
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		if e.EntityURN != "" && (e.UniversalName == slug || name == "") {
			return e.EntityURN, e.Name
		}
	}
	return "", ""
}

// parseCompanyFull maps the fat org response onto our Company schema.
// Field names verified against the real NVIDIA payload (_dev/company_full_raw.json).
func parseCompanyFull(body []byte, co *Company) {
	var full struct {
		Included []json.RawMessage `json:"included"`
	}
	if json.Unmarshal(body, &full) != nil {
		return
	}

	// pass 1: index entities by URN (for reference resolution, e.g. industry)
	type orgEntity struct {
		Type          string   `json:"$type"`
		EntityURN     string   `json:"entityUrn"`
		UniversalName string   `json:"universalName"`
		Name          string   `json:"name"`
		Tagline       string   `json:"tagline"`
		Description   string   `json:"description"`
		Website       string   `json:"websiteUrl"`
		FollowerCount int64    `json:"followerCount"`
		EmployeeCount int64    `json:"employeeCount"`
		EmployeeRange *struct {
			Start *int `json:"start"`
			End   *int `json:"end"`
		} `json:"employeeCountRange"`
		Headquarter *struct {
			Address *struct {
				City    string `json:"city"`
				GeoArea string `json:"geographicArea"`
				Country string `json:"country"`
			} `json:"address"`
		} `json:"headquarter"`
		FoundedOn *struct {
			Year int `json:"year"`
		} `json:"foundedOn"`
		Specialities []string        `json:"specialities"`
		Logo         json.RawMessage `json:"logoResolutionResult"`
		Cover        json.RawMessage `json:"croppedCoverImage"`
		IndustryRefs []string        `json:"*industryV2Taxonomy"`
		// Industry entity fields
		LocalizedName string `json:"localizedName"`
	}

	entities := map[string]orgEntity{}
	var ordered []orgEntity
	for _, raw := range full.Included {
		var e orgEntity
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		ordered = append(ordered, e)
		if e.EntityURN != "" {
			entities[e.EntityURN] = e
		}
	}

	// pass 2: find OUR org (universalName match wins; fall back to first Company)
	var org *orgEntity
	for i := range ordered {
		if ordered[i].UniversalName != "" && strings.HasSuffix(co.LinkedInURL, "/"+ordered[i].UniversalName+"/") {
			org = &ordered[i]
			break
		}
	}
	if org == nil {
		for i := range ordered {
			if strings.HasSuffix(ordered[i].Type, "organization.Company") && ordered[i].Name != "" {
				org = &ordered[i]
				break
			}
		}
	}
	if org == nil {
		return
	}

	co.Name = org.Name
	co.Tagline = org.Tagline
	co.Description = org.Description
	co.Website = org.Website
	co.Followers = org.FollowerCount
	co.StaffCount = org.EmployeeCount
	co.Specialties = org.Specialities
	if org.FoundedOn != nil {
		co.Founded = org.FoundedOn.Year
	}
	if org.EmployeeRange != nil && org.EmployeeRange.Start != nil {
		co.StaffRange = fmt.Sprintf("%d+", *org.EmployeeRange.Start)
		if org.EmployeeRange.End != nil {
			co.StaffRange = fmt.Sprintf("%d-%d", *org.EmployeeRange.Start, *org.EmployeeRange.End)
		}
	}
	if org.Headquarter != nil && org.Headquarter.Address != nil {
		a := org.Headquarter.Address
		co.Location = strings.Join(filterNonEmpty([]string{a.City, a.GeoArea, a.Country}), ", ")
	}
	co.LogoURLs = extractVectorImageURLs(org.Logo)
	co.CoverURLs = extractVectorImageURLs(org.Cover)

	// resolve industry reference -> IndustryV2 entity
	for _, ref := range org.IndustryRefs {
		if ind, ok := entities[ref]; ok {
			if ind.LocalizedName != "" {
				co.Industry = ind.LocalizedName
				break
			}
			if ind.Name != "" {
				co.Industry = ind.Name
				break
			}
		}
	}
	// followers can live on sibling entities (OrganizationalPage) — take any max
	if co.Followers == 0 {
		for _, e := range ordered {
			if e.FollowerCount > co.Followers {
				co.Followers = e.FollowerCount
			}
		}
	}
}

func filterNonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
