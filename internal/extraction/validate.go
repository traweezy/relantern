package extraction

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var topicIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

var allowedEventTypes = map[string]struct{}{
	"release": {}, "security": {}, "breaking_change": {}, "deprecation": {},
	"migration": {}, "preview": {}, "proposal": {}, "general": {}, "unknown": {},
}

var allowedLifecycleStates = map[string]struct{}{
	"stable": {}, "preview": {}, "release_candidate": {}, "deprecated": {},
	"end_of_life": {}, "unknown": {},
}

var allowedClaimTypes = map[string]struct{}{
	"release": {}, "version": {}, "date": {}, "breaking_change": {},
	"deprecation": {}, "security": {}, "migration_prerequisite": {},
	"example": {}, "compatibility": {}, "general": {},
}

var allowedConfidence = map[string]struct{}{
	"high": {}, "medium": {}, "low": {}, "unknown": {},
}

func DecodeAndValidate(raw string, spans []EvidenceSpan) (Output, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var output Output
	if err := decoder.Decode(&output); err != nil {
		return Output{}, fmt.Errorf("decode structured extraction output: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Output{}, err
	}
	if err := ValidateOutput(output, spans); err != nil {
		return Output{}, err
	}
	return output, nil
}

func ValidateOutput(output Output, spans []EvidenceSpan) error {
	if len(output.TopicIDs) > 20 || len(output.Claims) > MaximumClaims || len(output.Uncertainties) > 20 {
		return errors.New("structured extraction exceeds an array bound")
	}
	if _, allowed := allowedEventTypes[output.EventType]; !allowed {
		return fmt.Errorf("unsupported event type %q", output.EventType)
	}
	if _, allowed := allowedLifecycleStates[output.LifecycleState]; !allowed {
		return fmt.Errorf("unsupported lifecycle state %q", output.LifecycleState)
	}
	seenTopics := make(map[string]struct{}, len(output.TopicIDs))
	for _, topicID := range output.TopicIDs {
		if len(topicID) == 0 || len(topicID) > 64 || !topicIDPattern.MatchString(topicID) {
			return fmt.Errorf("invalid topic ID %q", topicID)
		}
		if _, duplicate := seenTopics[topicID]; duplicate {
			return fmt.Errorf("duplicate topic ID %q", topicID)
		}
		seenTopics[topicID] = struct{}{}
	}
	spanIndex := make(map[string]EvidenceSpan, len(spans))
	for _, span := range spans {
		if _, duplicate := spanIndex[span.ID]; duplicate {
			return fmt.Errorf("duplicate supplied evidence span %q", span.ID)
		}
		spanIndex[span.ID] = span
	}
	seenClaims := make(map[string]struct{}, len(output.Claims))
	for index, claim := range output.Claims {
		if err := validateClaim(claim, spanIndex); err != nil {
			return fmt.Errorf("claim %d: %w", index, err)
		}
		key := claim.ClaimType + "\x00" + claim.ClaimText + "\x00" + claim.NormalizedValue
		if _, duplicate := seenClaims[key]; duplicate {
			return fmt.Errorf("claim %d duplicates an earlier claim", index)
		}
		seenClaims[key] = struct{}{}
	}
	for index, uncertainty := range output.Uncertainties {
		if length := utf8.RuneCountInString(strings.TrimSpace(uncertainty)); length < 1 || length > 500 {
			return fmt.Errorf("uncertainty %d must contain 1 through 500 characters", index)
		}
	}
	return nil
}

func validateClaim(claim Claim, spanIndex map[string]EvidenceSpan) error {
	if _, allowed := allowedClaimTypes[claim.ClaimType]; !allowed {
		return fmt.Errorf("unsupported claim type %q", claim.ClaimType)
	}
	if length := utf8.RuneCountInString(strings.TrimSpace(claim.ClaimText)); length < 1 || length > 1000 {
		return errors.New("claim text must contain 1 through 1000 characters")
	}
	if utf8.RuneCountInString(claim.NormalizedValue) > 500 {
		return errors.New("normalized value exceeds 500 characters")
	}
	if _, allowed := allowedConfidence[claim.Confidence]; !allowed {
		return fmt.Errorf("unsupported confidence %q", claim.Confidence)
	}
	if len(claim.EvidenceSpanIDs) < 1 || len(claim.EvidenceSpanIDs) > 8 {
		return errors.New("every claim requires 1 through 8 evidence spans")
	}
	seen := make(map[string]struct{}, len(claim.EvidenceSpanIDs))
	for _, spanID := range claim.EvidenceSpanIDs {
		if _, exists := spanIndex[spanID]; !exists {
			return fmt.Errorf("claim references unknown evidence span %q", spanID)
		}
		if _, duplicate := seen[spanID]; duplicate {
			return fmt.Errorf("claim repeats evidence span %q", spanID)
		}
		seen[spanID] = struct{}{}
	}
	if claim.ClaimType == "date" && claim.NormalizedValue != "" {
		parsed, err := time.Parse("2006-01-02", claim.NormalizedValue)
		if err != nil || parsed.Format("2006-01-02") != claim.NormalizedValue {
			return errors.New("date normalized values must use YYYY-MM-DD")
		}
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing structured output: %w", err)
	}
	return errors.New("structured extraction output contains multiple JSON values")
}
