package linkedin

import "testing"

// Small synthetic normalized fixture: profile + certs collection with a dupe.
const dashFixture = `{"data":{}, "included":[
{"$type":"com.linkedin.voyager.dash.identity.profile.Profile","entityUrn":"urn:li:fsd_profile:X","summary":"  hello world  ","*profileCertifications":"urn:li:collectionResponse:C","*profileSkills":"urn:li:collectionResponse:S","*profileEducations":"urn:li:collectionResponse:E","*profileLanguages":"urn:li:collectionResponse:L","*profilePositionGroups":"urn:li:collectionResponse:P"},
{"$type":"com.linkedin.restli.common.CollectionResponse","entityUrn":"urn:li:collectionResponse:C","*elements":["urn:li:fsd_profileCertification:(X,1)","urn:li:fsd_profileCertification:(X,2)","urn:li:fsd_profileCertification:(X,3)"]},
{"$type":"com.linkedin.voyager.dash.identity.profile.Certification","entityUrn":"urn:li:fsd_profileCertification:(X,1)","name":"Marketing Insider ","authority":"LinkedIn","dateRange":{"start":{"year":2022,"month":10}}},
{"$type":"com.linkedin.voyager.dash.identity.profile.Certification","entityUrn":"urn:li:fsd_profileCertification:(X,2)","name":"Marketing Insider","authority":"LinkedIn","dateRange":{"start":{"year":2022,"month":10}}},
{"$type":"com.linkedin.voyager.dash.identity.profile.Certification","entityUrn":"urn:li:fsd_profileCertification:(X,3)","name":"Instagram Creator","authority":"Instagram","dateRange":{"start":{"year":2021}}},
{"$type":"com.linkedin.restli.common.CollectionResponse","entityUrn":"urn:li:collectionResponse:S","*elements":["urn:li:fsd_skill:(X,9)"]},
{"$type":"com.linkedin.voyager.dash.identity.profile.Skill","entityUrn":"urn:li:fsd_skill:(X,9)","name":"Digital Marketing"},
{"$type":"com.linkedin.restli.common.CollectionResponse","entityUrn":"urn:li:collectionResponse:L","*elements":["urn:li:fsd_profileLanguage:(X,1)"]},
{"$type":"com.linkedin.voyager.dash.identity.profile.Language","entityUrn":"urn:li:fsd_profileLanguage:(X,1)","name":"English ","proficiency":"FULL_PROFESSIONAL"},
{"$type":"com.linkedin.restli.common.CollectionResponse","entityUrn":"urn:li:collectionResponse:E","*elements":["urn:li:fsd_profileEducation:(X,1)"]},
{"$type":"com.linkedin.voyager.dash.identity.profile.Education","entityUrn":"urn:li:fsd_profileEducation:(X,1)","schoolName":"ITM","degreeName":"MBA","fieldOfStudy":"Marketing","dateRange":{"start":{"year":2015},"end":{"year":2017}}},
{"$type":"com.linkedin.restli.common.CollectionResponse","entityUrn":"urn:li:collectionResponse:P","*elements":["urn:li:fsd_profilePositionGroup:(X,g1)"]},
{"$type":"com.linkedin.voyager.dash.identity.profile.PositionGroup","entityUrn":"urn:li:fsd_profilePositionGroup:(X,g1)","companyName":"Acme","*profilePositionInPositionGroup":"urn:li:collectionResponse:PP"},
{"$type":"com.linkedin.restli.common.CollectionResponse","entityUrn":"urn:li:collectionResponse:PP","*elements":["urn:li:fsd_profilePosition:(X,p1)"]},
{"$type":"com.linkedin.voyager.dash.identity.profile.Position","entityUrn":"urn:li:fsd_profilePosition:(X,p1)","title":"CEO","companyName":"Acme","locationName":"India","*employmentType":"urn:li:fsd_employmentType:12","dateRange":{"start":{"year":2018,"month":11}}},
{"$type":"com.linkedin.voyager.dash.identity.profile.EmploymentType","entityUrn":"urn:li:fsd_employmentType:12","name":"Full-time"}
]}`

func TestParseDashProfile(t *testing.T) {
	dp, err := parseDashProfile([]byte(dashFixture))
	if err != nil {
		t.Fatalf("parseDashProfile: %v", err)
	}

	if dp.Summary != "hello world" {
		t.Errorf("Summary = %q (want trimmed)", dp.Summary)
	}

	// certs: dupe collapsed (same title+issuer+date), trailing space trimmed
	if len(dp.Certifications) != 2 {
		t.Fatalf("certs = %d, want 2: %+v", len(dp.Certifications), dp.Certifications)
	}
	if dp.Certifications[0].Title != "Marketing Insider" || dp.Certifications[0].IssuedDate != "Oct 2022" {
		t.Errorf("cert[0] wrong: %+v", dp.Certifications[0])
	}
	if dp.Certifications[1].IssuedDate != "2021" {
		t.Errorf("cert[1] year-only date wrong: %+v", dp.Certifications[1])
	}

	// skills
	if len(dp.Skills) != 1 || dp.Skills[0] != "Digital Marketing" {
		t.Errorf("skills wrong: %v", dp.Skills)
	}

	// languages: enum mapped + name trimmed
	if len(dp.Languages) != 1 || dp.Languages[0].Name != "English" ||
		dp.Languages[0].Proficiency != "Full professional proficiency" {
		t.Errorf("languages wrong: %+v", dp.Languages)
	}

	// education: degree + field of study joined UI-style
	if len(dp.Education) != 1 || dp.Education[0].Degree != "MBA, Marketing" ||
		dp.Education[0].DateRange != "2015 - 2017" {
		t.Errorf("education wrong: %+v", dp.Education)
	}

	// experience: group->positions flattened, employmentType ref resolved
	if len(dp.Experience) != 1 {
		t.Fatalf("experience = %d, want 1", len(dp.Experience))
	}
	e := dp.Experience[0]
	if e.Title != "CEO" || e.Company != "Acme" || e.EmploymentType != "Full-time" ||
		e.DateRange != "Nov 2018 - Present" || e.Location != "India" {
		t.Errorf("experience wrong: %+v", e)
	}
}

func TestParseDashProfileNoProfile(t *testing.T) {
	if _, err := parseDashProfile([]byte(`{"included":[]}`)); err == nil {
		t.Error("expected error for empty included[]")
	}
}

func TestDashDateFormatting(t *testing.T) {
	if got := (dashDate{Year: 2018, Month: 11}).String(); got != "Nov 2018" {
		t.Errorf("with month = %q", got)
	}
	if got := (dashDate{Year: 2015}).String(); got != "2015" {
		t.Errorf("year only = %q", got)
	}
	if got := (dashDate{}).String(); got != "" {
		t.Errorf("zero date = %q, want empty", got)
	}
	// open-ended position range -> Present
	dr, from, to := dashDateRange{Start: dashDate{Year: 2018, Month: 11}}.parts(true)
	if dr != "Nov 2018 - Present" || from != "Nov 2018" || to != "Present" {
		t.Errorf("open range = %q/%q/%q", dr, from, to)
	}
	// education range without end stays bare (no invented "Present")
	dr, _, to = dashDateRange{Start: dashDate{Year: 2015}}.parts(false)
	if dr != "2015" || to != "" {
		t.Errorf("open edu range = %q to=%q", dr, to)
	}
}
