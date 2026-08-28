package linkedin

import (
	"fmt"
	"strings"
)

// URLType classifies the kind of LinkedIn page an URL points at.
type URLType int

const (
	URLTypeProfile URLType = iota // /in/<slug>/
	URLTypeCompany                // /company/<slug>/
	URLTypeSchool                 // /school/<slug>/
	URLTypeUnsupported            // everything else
)

func (t URLType) String() string {
	switch t {
	case URLTypeProfile:
		return "profile"
	case URLTypeCompany:
		return "company"
	case URLTypeSchool:
		return "school"
	default:
		return "unsupported"
	}
}

// ClassifyURL normalizes a LinkedIn URL (protocol optional, trailing junk
// tolerated) and tells us what kind of page it is + its slug.
//
//	"linkedin.com/in/islaniaaayush/?foo=1" -> (profile, "islaniaaayush")
//	"https://www.linkedin.com/company/nvidia/" -> (company, "nvidia")
func ClassifyURL(raw string) (URLType, string, error) {
	u := strings.TrimSpace(raw)
	for _, p := range []string{"https://", "http://"} {
		u = strings.TrimPrefix(u, p)
	}
	u = strings.TrimPrefix(u, "www.")
	u = strings.TrimPrefix(u, "linkedin.com")
	u = strings.Trim(u, "/")
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}

	parts := strings.SplitN(u, "/", 3)
	if len(parts) < 2 || parts[1] == "" {
		return URLTypeUnsupported, "", fmt.Errorf("no slug in URL: %q", raw)
	}
	slug := parts[1]

	switch parts[0] {
	case "in":
		return URLTypeProfile, slug, nil
	case "company":
		return URLTypeCompany, slug, nil
	case "school":
		return URLTypeSchool, slug, nil
	default:
		return URLTypeUnsupported, "", fmt.Errorf(
			"unsupported LinkedIn URL type %q (supported: /in/, /company/, /school/)", parts[0])
	}
}
