package extraction

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type outlineBlock struct {
	Kind            string `json:"kind"`
	Anchor          string `json:"anchor"`
	NormalizedStart int    `json:"normalized_start"`
	NormalizedEnd   int    `json:"normalized_end"`
}

func BuildEvidenceSpans(normalized string, outlineJSON []byte) ([]EvidenceSpan, error) {
	if normalized == "" || len(normalized) > MaximumNormalizedBytes || !utf8.ValidString(normalized) {
		return nil, fmt.Errorf("normalized evidence must contain 1 through %d valid UTF-8 bytes", MaximumNormalizedBytes)
	}
	var blocks []outlineBlock
	if err := json.Unmarshal(outlineJSON, &blocks); err != nil {
		return nil, fmt.Errorf("decode normalized outline: %w", err)
	}
	if len(blocks) == 0 {
		blocks = []outlineBlock{{Kind: "document", Anchor: "/document", NormalizedEnd: len(normalized)}}
	}
	spans := make([]EvidenceSpan, 0, len(blocks))
	previousEnd := 0
	for _, block := range blocks {
		if block.NormalizedStart < previousEnd || block.NormalizedStart < 0 ||
			block.NormalizedEnd <= block.NormalizedStart || block.NormalizedEnd > len(normalized) {
			return nil, errors.New("normalized outline contains invalid or overlapping offsets")
		}
		if !utf8.ValidString(normalized[block.NormalizedStart:block.NormalizedEnd]) {
			return nil, errors.New("normalized outline splits a UTF-8 sequence")
		}
		sectionPath := strings.TrimSpace(block.Anchor)
		if sectionPath == "" {
			sectionPath = "/" + strings.TrimSpace(block.Kind)
		}
		if sectionPath == "/" {
			sectionPath = "/document"
		}
		for start := block.NormalizedStart; start < block.NormalizedEnd; {
			end := boundedSpanEnd(normalized, start, block.NormalizedEnd)
			spans = append(spans, EvidenceSpan{
				ID:          fmt.Sprintf("span_%04d", len(spans)+1),
				SectionPath: sectionPath,
				StartOffset: start,
				EndOffset:   end,
				Text:        normalized[start:end],
			})
			if len(spans) > MaximumEvidenceSpans {
				return nil, fmt.Errorf("normalized evidence exceeds %d spans", MaximumEvidenceSpans)
			}
			start = end
		}
		previousEnd = block.NormalizedEnd
	}
	if len(spans) == 0 {
		return nil, errors.New("normalized evidence contains no addressable spans")
	}
	return spans, nil
}

func boundedSpanEnd(value string, start int, blockEnd int) int {
	if blockEnd-start <= MaximumEvidenceSpanBytes {
		return blockEnd
	}
	end := start + MaximumEvidenceSpanBytes
	for end > start && !utf8.ValidString(value[start:end]) {
		end--
	}
	for cursor := end; cursor > start+MaximumEvidenceSpanBytes/2; cursor-- {
		if unicode.IsSpace(rune(value[cursor-1])) {
			return cursor
		}
	}
	return end
}

func EncodeEvidenceInput(title string, sourceID string, sourceTier string, spans []EvidenceSpan) (string, error) {
	payload, err := json.Marshal(struct {
		Title      string         `json:"title"`
		SourceID   string         `json:"source_id"`
		SourceTier string         `json:"source_tier"`
		Spans      []EvidenceSpan `json:"spans"`
	}{Title: title, SourceID: sourceID, SourceTier: sourceTier, Spans: spans})
	if err != nil {
		return "", fmt.Errorf("encode untrusted evidence: %w", err)
	}
	return "UNTRUSTED_EVIDENCE\n" + string(payload), nil
}
