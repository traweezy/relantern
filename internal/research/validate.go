package research

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

func EncodeValidatedFacts(title string, clusterID string, claims []ClaimFact) (string, []byte, error) {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(clusterID) == "" {
		return "", nil, errors.New("research input requires a title and cluster ID")
	}
	if len(claims) == 0 || len(claims) > MaximumClaims {
		return "", nil, fmt.Errorf("research input must contain 1 through %d claims", MaximumClaims)
	}
	ordered := append([]ClaimFact(nil), claims...)
	sort.Slice(ordered, func(left int, right int) bool { return ordered[left].ID < ordered[right].ID })
	seen := make(map[string]struct{}, len(ordered))
	for _, claim := range ordered {
		if err := validateClaimFact(claim); err != nil {
			return "", nil, err
		}
		if _, duplicate := seen[claim.ID]; duplicate {
			return "", nil, fmt.Errorf("duplicate validated claim ID %q", claim.ID)
		}
		seen[claim.ID] = struct{}{}
	}
	payload := struct {
		ClusterID string      `json:"cluster_id"`
		Title     string      `json:"title"`
		Claims    []ClaimFact `json:"validated_claims"`
	}{ClusterID: clusterID, Title: title, Claims: ordered}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("encode validated research facts: %w", err)
	}
	input := "VALIDATED_FACTS\n" + string(encoded)
	if len(input) > MaximumInputBytes {
		return "", nil, fmt.Errorf("research input exceeds %d bytes", MaximumInputBytes)
	}
	digest := sha256.Sum256([]byte(input))
	return input, digest[:], nil
}

func DecodeAndValidate(raw string, claims []ClaimFact, sources []Source, allowed []string, blocked []string) (Output, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var output Output
	if err := decoder.Decode(&output); err != nil {
		return Output{}, fmt.Errorf("decode research output: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Output{}, err
	}
	if err := ValidateOutput(output, claims, sources, allowed, blocked); err != nil {
		return Output{}, err
	}
	return output, nil
}

func ValidateOutput(output Output, claims []ClaimFact, sources []Source, allowed []string, blocked []string) error {
	if !boundedText(output.Headline, 180) || !boundedText(output.Summary, 2_000) ||
		!boundedText(output.WhyItMatters, 2_000) || !boundedText(output.RecommendedAction, 1_500) {
		return errors.New("research brief text is empty, invalid UTF-8, or exceeds its bound")
	}
	if !allowedValue(output.Confidence, "high", "medium", "low", "unknown") {
		return fmt.Errorf("unsupported research confidence %q", output.Confidence)
	}
	if len(output.Assertions) == 0 || len(output.Assertions) > 25 || len(output.Uncertainties) > 20 {
		return errors.New("research output assertion or uncertainty count is outside bounds")
	}
	claimIndex := make(map[string]ClaimFact, len(claims))
	for _, claim := range claims {
		claimIndex[claim.ID] = claim
	}
	sourceIndex := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		validated, err := ValidateSourceURL(source.URL, allowed, blocked)
		if err != nil {
			return fmt.Errorf("validate returned research source: %w", err)
		}
		if validated.Domain != source.Domain {
			return fmt.Errorf("research source domain mismatch for %q", source.URL)
		}
		sourceIndex[source.URL] = struct{}{}
	}
	for index, assertion := range output.Assertions {
		if !boundedText(assertion.Text, 1_000) || len(assertion.ClaimIDs) == 0 || len(assertion.ClaimIDs) > 12 || len(assertion.SourceURLs) > 12 {
			return fmt.Errorf("research assertion %d is outside bounds", index)
		}
		if err := validateReferences(assertion, claimIndex, sourceIndex); err != nil {
			return fmt.Errorf("research assertion %d: %w", index, err)
		}
	}
	for _, uncertainty := range output.Uncertainties {
		if !boundedText(uncertainty, 500) {
			return errors.New("research uncertainty is empty, invalid UTF-8, or exceeds 500 characters")
		}
	}
	return nil
}

func ValidateSourceURL(raw string, allowed []string, blocked []string) (Source, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return Source{}, fmt.Errorf("research source %q must be an absolute HTTPS URL without credentials or fragments", raw)
	}
	domain := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if domain == "" || len(domain) > 253 {
		return Source{}, fmt.Errorf("research source %q has an invalid domain", raw)
	}
	for _, candidate := range blocked {
		if domainMatches(domain, candidate) {
			return Source{}, fmt.Errorf("research source domain %q is blocked", domain)
		}
	}
	approved := false
	for _, candidate := range allowed {
		if domainMatches(domain, candidate) {
			approved = true
			break
		}
	}
	if !approved {
		return Source{}, fmt.Errorf("research source domain %q is not approved", domain)
	}
	return Source{URL: raw, Domain: domain}, nil
}

func validateClaimFact(claim ClaimFact) error {
	if strings.TrimSpace(claim.ID) == "" || strings.TrimSpace(claim.ItemID) == "" || strings.TrimSpace(claim.RevisionID) == "" ||
		!boundedText(claim.ClaimText, 1_000) || len(claim.NormalizedValue) > 500 {
		return errors.New("validated research claim is incomplete or outside bounds")
	}
	if claim.VerificationState != "verified_span" {
		return fmt.Errorf("claim %q is not evidence-verified", claim.ID)
	}
	if !allowedValue(claim.SourceTier, "T0", "T1") {
		return fmt.Errorf("claim %q is not from an authoritative source tier", claim.ID)
	}
	parsed, err := url.Parse(claim.SourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("claim %q has an invalid source URL", claim.ID)
	}
	return nil
}

func validateReferences(assertion Assertion, claims map[string]ClaimFact, sources map[string]struct{}) error {
	claimSeen := make(map[string]struct{}, len(assertion.ClaimIDs))
	for _, claimID := range assertion.ClaimIDs {
		if _, exists := claims[claimID]; !exists {
			return fmt.Errorf("claim ID %q was not supplied", claimID)
		}
		if _, duplicate := claimSeen[claimID]; duplicate {
			return fmt.Errorf("claim ID %q is duplicated", claimID)
		}
		claimSeen[claimID] = struct{}{}
	}
	sourceSeen := make(map[string]struct{}, len(assertion.SourceURLs))
	for _, sourceURL := range assertion.SourceURLs {
		if _, exists := sources[sourceURL]; !exists {
			return fmt.Errorf("source URL %q was not returned by web search", sourceURL)
		}
		if _, duplicate := sourceSeen[sourceURL]; duplicate {
			return fmt.Errorf("source URL %q is duplicated", sourceURL)
		}
		sourceSeen[sourceURL] = struct{}{}
	}
	return nil
}

func domainMatches(domain string, configured string) bool {
	candidate := strings.ToLower(strings.Trim(strings.TrimSpace(configured), "."))
	return candidate != "" && (domain == candidate || strings.HasSuffix(domain, "."+candidate))
}

func boundedText(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

func allowedValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode trailing research output: %w", err)
	}
	if len(bytes.TrimSpace(trailing)) > 0 {
		return errors.New("research output contains trailing JSON")
	}
	return nil
}
