package linkedin

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Contact info lives behind the "Contact info" overlay — a navigation action
// (NOT a component call), captured in recon round 4. Returns a small Flight
// payload (~18KB) with whatever the member shares with our account: websites/
// socials sometimes, email/phone mostly for 1st-degree connections. For
// 3rd-degree profiles the overlay is usually just the profile URL — then
// ParseContactInfo returns nil and the field is omitted from the response.
const rscContactOverlayURL = "https://www.linkedin.com/flagship-web/rsc-action/actions/navigation" +
	"?screenId=com.linkedin.sdui.flagshipnav.profile.ProfileContactDetailsOverlay" +
	"&sduiid=com.linkedin.sdui.flagshipnav.profile.ProfileContactDetailsOverlay"

//go:embed contact_body_template.json
var contactBodyTemplate string

// ContactInfo is the public schema for the contact-info overlay. All fields
// optional — LinkedIn only serves what the member shares with the viewer.
type ContactInfo struct {
	Email    string   `json:"email,omitempty"`
	Phone    string   `json:"phone,omitempty"`
	Birthday string   `json:"birthday,omitempty"`
	Twitter  string   `json:"twitter,omitempty"`
	Websites []string `json:"websites,omitempty"`
}

// GetContactInfo replays the contact-info overlay navigation call. Needs the
// person's given/family name (from the topcard) in addition to the vanity.
func (c *Client) GetContactInfo(vanity, givenName, familyName string) (string, error) {
	body := strings.ReplaceAll(contactBodyTemplate, "{{VANITY}}", vanity)
	body = strings.ReplaceAll(body, "{{GIVEN_NAME}}", givenName)
	body = strings.ReplaceAll(body, "{{FAMILY_NAME}}", familyName)

	req, err := c.newRequest("POST", rscContactOverlayURL, bytes.NewReader([]byte(body)))
	if err != nil {
		return "", err
	}
	// Header set mirrored from the captured real overlay call (round 4).
	// Note the referer is the OVERLAY URL, and x-li-layout-tree is replayed
	// verbatim like our other captured tracking headers.
	req.Header.Set("content-type", "application/json")
	req.Header.Set("referer", "https://www.linkedin.com/in/"+vanity+"/overlay/contact-info/")
	req.Header.Set("x-li-rsc-stream", "true")
	req.Header.Set("x-li-application-version", liAppVersion)
	req.Header.Set("x-li-anchor-page-key", "d_flagship3_profile_view_base")
	req.Header.Set("x-li-page-instance", "urn:li:page:d_flagship3_profile_view_base;TKAfGsUtQ1atc3Zckuzsew==")
	req.Header.Set("x-li-page-instance-tracking-id", "TKAfGsUtQ1atc3Zckuzsew==")
	req.Header.Set("x-li-track", liTrack)
	req.Header.Set("x-li-layout-tree", `["com.linkedin.sdui.flagshipnav.profile.Profile#345f13a","com.linkedin.sdui.flagshipnav.home.Home#0","a15eca777c146d37da0475b8f19e5d56"]`)

	resp, err := c.doWithRetry(req)
	if err != nil {
		return "", fmt.Errorf("contact-info request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("contact-info status %d: %.300s", resp.StatusCode, respBody)
	}
	return string(respBody), nil
}

var reEmail = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// contactChrome is overlay UI scaffolding, not data. ("Follow"/"Following"
// already live in the global noiseLeaves.)
var contactChrome = map[string]bool{
	"MainContent": true, "Connect": true, "Message": true, "Close": true,
	"profile_view_base_contact_details": true,
}

// contactLabels are the overlay's section labels; the value rides in the
// next leaf (verified: label-above-value SDUI layout).
var contactLabels = map[string]bool{
	"Email": true, "Phone": true, "Birthday": true, "Twitter": true,
	"Website": true, "Websites": true, "Address": true, "Connected": true,
}

// ParseContactInfo turns the overlay's text leaves into structured contact
// data. Returns nil when nothing beyond chrome/profile-URL was shared (the
// common case for 3rd-degree profiles) so the field is omitted entirely.
func ParseContactInfo(leaves []string) *ContactInfo {
	info := &ContactInfo{}
	label := ""
	for _, l := range leaves {
		if isNoise(l) || contactChrome[l] ||
			strings.HasSuffix(l, "’s profile") || strings.HasSuffix(l, "'s profile") {
			continue
		}
		if strings.HasPrefix(l, "linkedin.com/in/") {
			continue // profile URL — redundant with linkedin_url
		}
		if contactLabels[l] {
			label = l
			continue
		}
		switch {
		case label == "Email" || (label == "" && reEmail.MatchString(l)):
			if info.Email == "" {
				info.Email = l
			}
		case label == "Phone":
			info.Phone = l
		case label == "Birthday":
			info.Birthday = l
		case label == "Twitter" || strings.Contains(l, "twitter.com"):
			info.Twitter = l
		case label == "Website" || label == "Websites" || looksLikeURL(l):
			info.Websites = append(info.Websites, l)
		}
		label = ""
	}
	if info.Email == "" && info.Phone == "" && info.Birthday == "" &&
		info.Twitter == "" && len(info.Websites) == 0 {
		return nil
	}
	return info
}

// looksLikeURL classifies label-free leaves: single-token strings with a dot
// ("example.com", "https://x.dev") — sentences never qualify.
func looksLikeURL(s string) bool {
	if strings.ContainsAny(s, " \t") {
		return false
	}
	return strings.HasPrefix(s, "http") || strings.HasPrefix(s, "www.") ||
		strings.Contains(s, ".")
}
