package digest

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func Render(input RenderInput) (DigestPayload, []byte, error) {
	if input.Channel != ChannelDashboard && input.Channel != ChannelDiscord && input.Channel != ChannelEmail {
		return DigestPayload{}, nil, errors.New("unsupported digest channel")
	}
	if input.LocalDate == "" || !input.WindowEnd.After(input.WindowStart) || input.GeneratedAt.IsZero() {
		return DigestPayload{}, nil, errors.New("digest render input is incomplete")
	}
	items := make([]DigestRenderedItem, 0, len(input.Items))
	for _, item := range input.Items {
		items = append(items, DigestRenderedItem{
			Type: item.Type, ID: item.ID, Headline: boundedText(item.Headline, 180),
			Summary: boundedText(item.Summary, 600), Action: boundedText(item.Action, 300),
			Signal: item.Signal, Reason: boundedText(item.Reason, 500), AppPath: item.AppPath,
			SourceURL: item.SourceURL, Score: item.Score,
		})
	}
	summary := strings.TrimSpace(input.ExecutiveSummary)
	if summary == "" {
		if len(items) == 0 {
			summary = "No material, evidence-backed changes met this digest's reviewed threshold."
		} else {
			summary = fmt.Sprintf("%d evidence-backed changes are ready for owner review.", len(items))
		}
	}
	payload := DigestPayload{
		Channel: input.Channel, Title: "Relantern morning brief · " + input.LocalDate,
		ExecutiveSummary: boundedText(summary, 2000), LocalDate: input.LocalDate,
		WindowStart: input.WindowStart.UTC(), WindowEnd: input.WindowEnd.UTC(),
		GeneratedAt: input.GeneratedAt.UTC(), Items: items,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return DigestPayload{}, nil, fmt.Errorf("encode rendered digest: %w", err)
	}
	if len(encoded) > 64<<10 {
		return DigestPayload{}, nil, errors.New("rendered digest exceeds 64 KiB")
	}
	digest := sha256.Sum256(encoded)
	return payload, digest[:], nil
}

func boundedText(value string, maximum int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	if maximum <= 1 {
		return string(runes[:maximum])
	}
	return strings.TrimSpace(string(runes[:maximum-1])) + "…"
}
