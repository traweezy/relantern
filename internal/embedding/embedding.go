package embedding

import (
	"crypto/sha256"
	"errors"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	DefaultModelID    = "text-embedding-3-small"
	DefaultDimensions = 1536
	MaximumInputBytes = 100_000
)

type Vector []float32

func ValidateVector(vector Vector, dimensions int) error {
	if dimensions < 1 || dimensions > 4096 {
		return errors.New("embedding dimensions must be between 1 and 4096")
	}
	if len(vector) != dimensions {
		return errors.New("embedding vector does not match configured dimensions")
	}
	var squaredNorm float64
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return errors.New("embedding vector contains a non-finite value")
		}
		squaredNorm += float64(value) * float64(value)
	}
	if squaredNorm == 0 {
		return errors.New("embedding vector must have a non-zero norm")
	}
	return nil
}

func ValidateInput(input string) error {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return errors.New("embedding input is required")
	}
	if len([]byte(trimmed)) > MaximumInputBytes {
		return errors.New("embedding input exceeds the 100000-byte limit")
	}
	return nil
}

func ContentDigest(input string) [sha256.Size]byte {
	return sha256.Sum256([]byte(input))
}

// BoundedInput is the canonical input used for a revision's embedding. The
// search index uses the same digest to pin an immutable vector to its text.
func BoundedInput(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || !utf8.ValidString(trimmed) {
		return "", errors.New("normalized revision does not contain valid embedding input")
	}
	if err := ValidateInput(trimmed); err == nil {
		return trimmed, nil
	}
	payload := []byte(trimmed)
	if len(payload) <= MaximumInputBytes {
		return "", errors.New("normalized revision embedding input is invalid")
	}
	payload = payload[:MaximumInputBytes]
	for !utf8.Valid(payload) {
		payload = payload[:len(payload)-1]
	}
	return string(payload), nil
}
