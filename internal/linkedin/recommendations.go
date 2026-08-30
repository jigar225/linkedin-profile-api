package linkedin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GetRecommendations fetches the RECEIVED recommendations list from the
// classic voyager REST endpoint — the one piece of the old identity API
// still alive (verified Aug 2026: q=received → 200; q-less → 400;
// profileContactInfo → 410 Gone). Returns the full first page — the RSC
// stream only ever rendered a 4-card preview.
func (c *Client) GetRecommendations(vanity string) ([]Recommendation, error) {
	url := "https://www.linkedin.com/voyager/api/identity/profiles/" + vanity + "/recommendations?q=received"

	req, err := c.newRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("recommendations request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("recommendations status %d: %.300s", resp.StatusCode, body)
	}
	if dir := os.Getenv("LINKEDIN_DEBUG_DIR"); dir != "" {
		base := filepath.Join(dir, vanity)
		if err := os.MkdirAll(base, 0755); err == nil {
			os.WriteFile(filepath.Join(base, "recommendations.json"), body, 0644)
		}
	}
	return parseRecommendations(body)
}

// recoElement is one voyager recommendations-list entry (classic REST shape,
// mini-profiles inlined — no normalized `included` resolution needed).
type recoElement struct {
	RecommendationText string `json:"recommendationText"`
	Relationship       string `json:"relationship"` // ENUM (WORKED_IN_DIFFERENT_GROUPS, …)
	Created            int64  `json:"created"`      // ms epoch
	Status             string `json:"status"`       // VISIBLE, …
	Recommender        struct {
		FirstName  string `json:"firstName"`
		LastName   string `json:"lastName"`
		Occupation string `json:"occupation"` // the recommender's headline
	} `json:"recommender"`
}

// parseRecommendations decodes the voyager response into our schema.
func parseRecommendations(body []byte) ([]Recommendation, error) {
	var env struct {
		Elements []recoElement `json:"elements"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("recommendations JSON: %w", err)
	}

	out := []Recommendation{}
	for _, el := range env.Elements {
		if el.Status != "" && el.Status != "VISIBLE" {
			continue // hidden recos are the member's choice — respect it
		}
		date := ""
		if el.Created > 0 {
			date = time.UnixMilli(el.Created).UTC().Format("January 2, 2006")
		}
		out = append(out, Recommendation{
			Recommender:  strings.TrimSpace(el.Recommender.FirstName + " " + el.Recommender.LastName),
			Headline:     el.Recommender.Occupation,
			Relationship: recoRelationshipText(el.Relationship),
			Date:         date,
			Text:         el.RecommendationText,
			Direction:    "received",
		})
	}
	return out, nil
}

// recoRelationshipText maps LinkedIn's relationship ENUMS to display text.
// Honest trade-off vs the retired RSC parse: the enum is GENERIC — it can't
// embed names ("X managed Y directly"). Unknown enums surface raw (visible,
// never guessed). Verified enums only — extend as new ones appear in dumps.
func recoRelationshipText(enum string) string {
	switch enum {
	case "WORKED_IN_SAME_GROUP":
		return "Worked together on the same team"
	case "WORKED_IN_DIFFERENT_GROUPS":
		return "Worked together but on different teams"
	case "RECOMMENDER_IS_CLIENT_OF_RECOMMENDEE":
		return "Was their client"
	case "MANAGED_RECOMMENDEE":
		return "Managed them directly"
	case "RECOMMENDEE_MANAGED_RECOMMENDER":
		return "Reported to them"
	case "STUDIED_TOGETHER":
		return "Studied together"
	default:
		return enum
	}
}
