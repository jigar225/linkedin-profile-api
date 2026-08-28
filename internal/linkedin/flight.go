package linkedin

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// React Flight payloads are line-delimited rows like:
//
//	15:["$","$L23",null,{"children":["Issued Sep 2024"], ...}]
//
// Human text hides in two spots: "children":["<text>"] leaves and
// proto.sdui {"stringValue":"<text>"} values. We harvest both, in reading order.
var (
	reChildren    = regexp.MustCompile(`"children":\["((?:[^"\\]|\\.)*)"\]`)
	reStringValue = regexp.MustCompile(`"stringValue":"((?:[^"\\]|\\.)*)"`)
	// Rich-text paragraphs (About, descriptions) ride inside react.fragment
	// children: "children":[null,"<paragraph>"] or [["$","br",null,{}],"<paragraph>"].
	reFragNull = regexp.MustCompile(`"children":\[null,"((?:[^"\\]|\\.)*)"\]`)
	reFragBr   = regexp.MustCompile(`\["\$","br",null,\{\}\],"((?:[^"\\]|\\.)*)"`)
)

type textHit struct {
	pos  int
	text string
}

// ExtractFlightTexts pulls human-readable text leaves out of a React Flight
// payload, in the order they appear (reading order). This mirrors the
// "parse what the human sees" philosophy — immune to LinkedIn's schema churn.
func ExtractFlightTexts(flight string) []string {
	var hits []textHit
	for _, re := range []*regexp.Regexp{reChildren, reStringValue, reFragNull, reFragBr} {
		for _, m := range re.FindAllStringSubmatchIndex(flight, -1) {
			// m[2:4] = capture group 1 byte range
			raw := flight[m[2]:m[3]]
			if s, ok := unescapeJSONString(raw); ok {
				hits = append(hits, textHit{pos: m[0], text: s})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })

	var out []string
	for _, h := range hits {
		t := strings.TrimSpace(strings.Map(stripZeroWidth, h.text))
		if t == "" || strings.HasPrefix(t, "$L") || len(t) < 2 {
			continue
		}
		// skip adjacent duplicates (Flight repeats itself for a11y)
		if n := len(out); n > 0 && out[n-1] == t {
			continue
		}
		out = append(out, t)
	}
	return out
}

// unescapeJSONString decodes a raw JSON-string fragment (\", \n, …).
func unescapeJSONString(raw string) (string, bool) {
	var s string
	if err := json.Unmarshal([]byte(`"`+raw+`"`), &s); err != nil {
		return "", false
	}
	return s, true
}

// stripZeroWidth removes invisible format chars (U+200B/C/D, BOM) that
// LinkedIn embeds in Flight text — TrimSpace alone doesn't catch them,
// and they'd leak through as phantom "empty-looking" leaves.
func stripZeroWidth(r rune) rune {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\ufeff':
		return -1
	}
	return r
}
