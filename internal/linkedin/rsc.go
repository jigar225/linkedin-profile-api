package linkedin

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"strings"
)

// RSC component IDs — they name the profile section bundles the SPA asks for.
// Captured from real traffic. (Above = About/top area, Below = everything under Activity.)
const (
	RSCAboveActivity = "com.linkedin.sdui.generated.profile.dsl.impl.profileCardsAboveActivity"
	RSCExperienceOnly = "com.linkedin.sdui.generated.profile.dsl.impl.profileCardsExperienceOnly"
	// RSCBelowActivityBase is the prefix for the lazy-loaded section chunks:
	// Part1WithoutExp, Part2, Part3 … (numbering varies per profile).
	RSCBelowActivityBase = "com.linkedin.sdui.generated.profile.dsl.impl.profileCardsBelowActivity"
)

const rscComponentURL = "https://www.linkedin.com/flagship-web/rsc-action/actions/component?componentId="

// rscBodyTemplate is the EXACT request body LinkedIn's SPA sends for profile
// sections (captured in recon round 3), with two placeholders.
// The many BindingImpl entries are client-state references — LinkedIn expects
// them present, so we replay them verbatim. A stripped body returns 500.
//
//go:embed rsc_body_template.json
var rscBodyTemplate string

// GetProfileSection replays the exact RSC-action call LinkedIn's own SPA makes
// to render profile sections. Returns the raw React Flight payload.
func (c *Client) GetProfileSection(vanity, vieweeID, componentID string) (string, error) {
	body := strings.ReplaceAll(rscBodyTemplate, "{{VANITY}}", vanity)
	body = strings.ReplaceAll(body, "{{VIEWEE_ID}}", vieweeID)

	req, err := c.newRequest("POST", rscComponentURL+componentID, bytes.NewReader([]byte(body)))
	if err != nil {
		return "", err
	}
	// Header set mirrored from the captured real call.
	req.Header.Set("content-type", "application/json")
	req.Header.Set("referer", "https://www.linkedin.com/in/"+vanity+"/")
	req.Header.Set("x-li-rsc-stream", "true")
	req.Header.Set("x-li-application-version", "0.2.6951")
	req.Header.Set("x-li-anchor-page-key", "d_flagship3_profile_view_base")
	req.Header.Set("x-li-page-instance", "urn:li:page:d_flagship3_profile_view_base;fF3VyyBJQT25ia7GrNb2kA==")
	req.Header.Set("x-li-page-instance-tracking-id", "fF3VyyBJQT25ia7GrNb2kA==")
	req.Header.Set("x-li-track", `{"clientVersion":"0.2.6951","mpVersion":"0.2.6951","osName":"web","timezoneOffset":5.5,"timezone":"Asia/Calcutta","deviceFormFactor":"DESKTOP","mpName":"web","displayDensity":1,"displayWidth":1280,"displayHeight":720}`)

	resp, err := c.doWithRetry(req)
	if err != nil {
		return "", fmt.Errorf("rsc request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("rsc status %d: %.300s", resp.StatusCode, respBody)
	}
	return string(respBody), nil
}
