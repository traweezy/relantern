package digest

import (
	"bytes"
	"reflect"
	"testing"
	"time"
)

func TestRenderIsDeterministicAndBounded(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	input := RenderInput{
		Channel: ChannelDiscord, LocalDate: "2026-08-29",
		WindowStart: now.Add(-24 * time.Hour), WindowEnd: now.Add(-15 * time.Minute),
		GeneratedAt: now,
		Items: []DigestCandidate{{
			Type: "story", ID: "story", Headline: "A  headline\nwith spacing",
			Summary: "Evidence-backed summary", Action: "Review migration", Signal: "release",
			Reason: "Stable release", AppPath: "/story/story", SourceURL: "https://example.test/release",
			Score: 0.8,
		}},
	}
	first, firstHash, err := Render(input)
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := Render(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Items[0].Headline != "A headline with spacing" || first.ExecutiveSummary == "" || !reflect.DeepEqual(first, second) || !bytes.Equal(firstHash, secondHash) {
		t.Fatalf("render mismatch: first=%+v second=%+v", first, second)
	}
}

func TestRenderFallsBackWithoutModelSummary(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	payload, _, err := Render(RenderInput{
		Channel: ChannelEmail, LocalDate: "2026-08-29",
		WindowStart: now.Add(-24 * time.Hour), WindowEnd: now, GeneratedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 0 || payload.ExecutiveSummary == "" {
		t.Fatalf("empty fallback payload = %+v", payload)
	}
}
