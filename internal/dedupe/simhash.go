package dedupe

import (
	"hash/fnv"
	"math/bits"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var versionPattern = regexp.MustCompile(`(?i)\bv?(\d+\.\d+(?:\.\d+)?(?:[-+][0-9a-z.-]+)?)\b`)

func SimHash(text string) uint64 {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return 0
	}
	features := make([]string, 0, len(tokens)*2-1)
	features = append(features, tokens...)
	for index := 1; index < len(tokens); index++ {
		features = append(features, tokens[index-1]+"\x00"+tokens[index])
	}
	var weights [64]int
	for _, feature := range features {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(feature))
		value := hasher.Sum64()
		for bit := range 64 {
			if value&(uint64(1)<<bit) != 0 {
				weights[bit]++
			} else {
				weights[bit]--
			}
		}
	}
	var fingerprint uint64
	for bit, weight := range weights {
		if weight >= 0 {
			fingerprint |= uint64(1) << bit
		}
	}
	return fingerprint
}

func SimHashDistance(first uint64, second uint64) int {
	return bits.OnesCount64(first ^ second)
}

func NormalizeMetadata(value string) string {
	value = norm.NFKC.String(strings.ToLower(strings.TrimSpace(value)))
	return strings.Join(strings.FieldsFunc(value, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '+' && character != '#'
	}), " ")
}

func ExtractReleaseMetadata(title string) (string, string) {
	location := versionPattern.FindStringSubmatchIndex(title)
	if len(location) < 4 {
		return "", ""
	}
	version := strings.ToLower(title[location[2]:location[3]])
	prefix := strings.TrimSpace(title[:location[0]])
	prefix = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(prefix), "version"), "v"))
	prefix = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(prefix, "release"), "released"))
	packageName := NormalizeMetadata(prefix)
	if packageName == "" || len(strings.Fields(packageName)) > 5 {
		return "", version
	}
	return packageName, version
}

func tokenize(value string) []string {
	value = norm.NFKC.String(strings.ToLower(value))
	return strings.FieldsFunc(value, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '+' && character != '#'
	})
}
