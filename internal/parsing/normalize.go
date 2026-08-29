package parsing

import (
	"crypto/sha256"
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

var sentenceBoundary = regexp.MustCompile(`(?m)([^.!?\n]+[.!?]?|\n+)`)

var injectionMarkers = []string{
	"ignore previous instructions",
	"ignore all previous instructions",
	"reveal secrets",
	"reveal the system prompt",
	"developer message",
	"system message",
	"call a tool",
}

func finalize(document extractedDocument, source string) (Result, error) {
	blocks := make([]extractedBlock, 0, len(document.Blocks))
	warnings := append([]Warning(nil), document.Warnings...)
	title, titleInstructionsRemoved := stripEmbeddedInstructions(normalizeText(document.Title, false), false)
	if titleInstructionsRemoved {
		warnings = append(warnings, Warning{
			Code:   "embedded_instruction_removed",
			Detail: "Potential prompt instructions were removed from normalized title metadata; raw evidence remains immutable.",
			Anchor: "/metadata/title",
		})
	}
	author, authorInstructionsRemoved := stripEmbeddedInstructions(normalizeText(document.Author, false), false)
	if authorInstructionsRemoved {
		warnings = append(warnings, Warning{
			Code:   "embedded_instruction_removed",
			Detail: "Potential prompt instructions were removed from normalized author metadata; raw evidence remains immutable.",
			Anchor: "/metadata/author",
		})
	}
	for _, block := range document.Blocks {
		text := normalizeText(block.Text, block.Kind == "code")
		filtered, removed := stripEmbeddedInstructions(text, block.Kind == "code")
		if removed {
			warnings = append(warnings, Warning{
				Code:   "embedded_instruction_removed",
				Detail: "Potential prompt instructions were removed from normalized content; raw evidence remains immutable.",
				Anchor: block.Anchor,
			})
		}
		if filtered == "" {
			continue
		}
		block.Text = filtered
		blocks = append(blocks, block)
	}
	if navigationOnly(blocks) {
		return Result{}, parserError(ErrorEmptyDocument, "parser extraction is empty or navigation-only")
	}

	var normalized strings.Builder
	outline := make([]OutlineBlock, 0, len(blocks))
	offsetMap := make([]OffsetRange, 0, len(blocks))
	searchOffset := 0
	for index, block := range blocks {
		if index > 0 {
			normalized.WriteString("\n\n")
		}
		start := normalized.Len()
		normalized.WriteString(block.Text)
		end := normalized.Len()
		outline = append(outline, OutlineBlock{
			Kind:            block.Kind,
			Level:           block.Level,
			Anchor:          block.Anchor,
			NormalizedStart: start,
			NormalizedEnd:   end,
		})
		sourceStart, sourceEnd, approximate := locateSourceText(source, block.Text, searchOffset)
		if sourceEnd >= 0 {
			searchOffset = sourceEnd
		}
		offsetMap = append(offsetMap, OffsetRange{
			NormalizedStart: start,
			NormalizedEnd:   end,
			SourceStart:     sourceStart,
			SourceEnd:       sourceEnd,
			Approximate:     approximate,
		})
	}
	normalizedText := normalized.String()
	if strings.TrimSpace(normalizedText) == "" {
		return Result{}, parserError(ErrorEmptyDocument, "parser extraction is empty")
	}
	languageCode := normalizeLanguage(document.Language, normalizedText)
	warnings = canonicalWarnings(warnings)
	result := Result{
		ParserName:        document.ParserName,
		ParserVersion:     ParserVersion,
		Title:             title,
		Author:            author,
		Language:          languageCode,
		CanonicalURL:      document.CanonicalURL,
		SourcePublishedAt: document.SourcePublishedAt.UTC(),
		SourceUpdatedAt:   document.SourceUpdatedAt.UTC(),
		NormalizedText:    normalizedText,
		NormalizedSHA256:  sha256.Sum256([]byte(normalizedText)),
		Outline:           outline,
		OffsetMap:         offsetMap,
		Warnings:          warnings,
	}
	if err := ValidateResult(result); err != nil {
		return Result{}, parserError(ErrorInvalidDocument, "validate normalized parser result: %v", err)
	}
	return result, nil
}

func normalizeText(value string, preserveLines bool) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = norm.NFC.String(strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' {
			return character
		}
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value))
	if !preserveLines {
		return strings.Join(strings.Fields(value), " ")
	}
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = strings.TrimRightFunc(lines[index], unicode.IsSpace)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func stripEmbeddedInstructions(value string, preserveLines bool) (string, bool) {
	if value == "" {
		return "", false
	}
	removed := false
	parts := sentenceBoundary.FindAllString(value, -1)
	if len(parts) == 0 {
		parts = []string{value}
	}
	var kept strings.Builder
	for _, part := range parts {
		lower := strings.ToLower(part)
		instruction := false
		for _, marker := range injectionMarkers {
			if strings.Contains(lower, marker) {
				instruction = true
				break
			}
		}
		if instruction {
			removed = true
			continue
		}
		kept.WriteString(part)
	}
	if !removed {
		return value, false
	}
	return normalizeText(kept.String(), preserveLines), true
}

func navigationOnly(blocks []extractedBlock) bool {
	meaningful := 0
	for _, block := range blocks {
		for _, character := range block.Text {
			if unicode.IsLetter(character) || unicode.IsDigit(character) {
				meaningful++
			}
		}
	}
	return meaningful < 3
}

func locateSourceText(source string, normalized string, start int) (int, int, bool) {
	if normalized == "" {
		return -1, -1, true
	}
	if start >= 0 && start <= len(source) {
		if relative := strings.Index(source[start:], normalized); relative >= 0 {
			position := start + relative
			return position, position + len(normalized), false
		}
	}
	if position := strings.Index(source, normalized); position >= 0 {
		return position, position + len(normalized), false
	}
	return -1, -1, true
}

func normalizeLanguage(explicit string, text string) string {
	if explicit != "" {
		if tag, err := language.Parse(explicit); err == nil {
			base, _ := tag.Base()
			if base.String() != "und" {
				return base.String()
			}
		}
	}
	var latin, han, hiragana, katakana int
	for _, character := range text {
		switch {
		case unicode.In(character, unicode.Han):
			han++
		case unicode.In(character, unicode.Hiragana):
			hiragana++
		case unicode.In(character, unicode.Katakana):
			katakana++
		case unicode.In(character, unicode.Latin):
			latin++
		}
	}
	if hiragana+katakana >= 2 {
		return "ja"
	}
	if han >= 2 && han >= latin {
		return "zh"
	}
	if latin >= 12 {
		return "en"
	}
	return "und"
}

func canonicalWarnings(warnings []Warning) []Warning {
	if len(warnings) == 0 {
		return []Warning{}
	}
	seen := make(map[string]struct{}, len(warnings))
	result := make([]Warning, 0, len(warnings))
	for _, warning := range warnings {
		key := warning.Code + "\x00" + warning.Anchor + "\x00" + warning.Detail
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, warning)
	}
	sort.Slice(result, func(first int, second int) bool {
		if result[first].Code != result[second].Code {
			return result[first].Code < result[second].Code
		}
		return result[first].Anchor < result[second].Anchor
	})
	return result
}

func validateOffsets(result Result) error {
	for _, offset := range result.OffsetMap {
		if offset.NormalizedStart < 0 || offset.NormalizedEnd < offset.NormalizedStart || offset.NormalizedEnd > len(result.NormalizedText) {
			return errors.New("normalized offset is outside normalized text")
		}
		if !utf8.ValidString(result.NormalizedText[offset.NormalizedStart:offset.NormalizedEnd]) {
			return errors.New("normalized offset splits invalid UTF-8")
		}
		if offset.SourceStart == -1 || offset.SourceEnd == -1 {
			if offset.SourceStart != -1 || offset.SourceEnd != -1 || !offset.Approximate {
				return errors.New("unlocated source offsets must be approximate and use -1 bounds")
			}
			continue
		}
		if offset.SourceStart < 0 || offset.SourceEnd < offset.SourceStart {
			return errors.New("source offset has invalid bounds")
		}
	}
	return nil
}

func ValidateResult(result Result) error {
	if result.ParserName == "" || result.ParserVersion == "" {
		return errors.New("parser name and version are required")
	}
	if result.NormalizedText == "" || !utf8.ValidString(result.NormalizedText) {
		return errors.New("normalized text must be non-empty valid UTF-8")
	}
	if sha256.Sum256([]byte(result.NormalizedText)) != result.NormalizedSHA256 {
		return errors.New("normalized SHA-256 does not match normalized text")
	}
	if len(result.Outline) == 0 || len(result.Outline) != len(result.OffsetMap) {
		return errors.New("outline and offset map must contain one entry per normalized block")
	}
	for index, block := range result.Outline {
		if block.Kind == "" || block.Anchor == "" {
			return errors.New("outline kind and anchor are required")
		}
		if block.NormalizedStart != result.OffsetMap[index].NormalizedStart || block.NormalizedEnd != result.OffsetMap[index].NormalizedEnd {
			return errors.New("outline and offset-map normalized bounds differ")
		}
	}
	return validateOffsets(result)
}
