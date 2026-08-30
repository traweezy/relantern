package release

import (
	"strings"
	"testing"
)

func TestValidateSignedTagAcceptsVerifiedAnnotatedTag(t *testing.T) {
	t.Parallel()
	reference := `{"ref":"refs/tags/v1.0.0","object":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","type":"tag"},"extra":true}`
	tagObject := `{"tag":"v1.0.0","object":{"sha":"0123456789abcdef0123456789abcdef01234567","type":"commit"},"verification":{"verified":true,"reason":"valid"}}`
	report, err := ValidateSignedTag(strings.NewReader(reference), strings.NewReader(tagObject), "v1.0.0", testReleaseSHA)
	if err != nil {
		t.Fatalf("ValidateSignedTag() error = %v", err)
	}
	if !report.Verified || report.CommitSHA != testReleaseSHA {
		t.Fatalf("ValidateSignedTag() report = %+v", report)
	}
}

func TestValidateSignedTagRejectsLightweightInvalidAndWrongTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		reference string
		tagObject string
	}{
		{
			name:      "lightweight",
			reference: `{"ref":"refs/tags/v1.0.0","object":{"sha":"0123456789abcdef0123456789abcdef01234567","type":"commit"}}`,
			tagObject: `{}`,
		},
		{
			name:      "invalid signature",
			reference: `{"ref":"refs/tags/v1.0.0","object":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","type":"tag"}}`,
			tagObject: `{"tag":"v1.0.0","object":{"sha":"0123456789abcdef0123456789abcdef01234567","type":"commit"},"verification":{"verified":false,"reason":"unknown_key"}}`,
		},
		{
			name:      "wrong target",
			reference: `{"ref":"refs/tags/v1.0.0","object":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","type":"tag"}}`,
			tagObject: `{"tag":"v1.0.0","object":{"sha":"1123456789abcdef0123456789abcdef01234567","type":"commit"},"verification":{"verified":true,"reason":"valid"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ValidateSignedTag(strings.NewReader(test.reference), strings.NewReader(test.tagObject), "v1.0.0", testReleaseSHA); err == nil {
				t.Fatal("ValidateSignedTag() accepted an invalid tag")
			}
		})
	}
}
