package intelligence

import (
	"context"
	"errors"
	"time"
)

var ErrStoryNotFound = errors.New("intelligence story not found")

type Source struct {
	Domain string `json:"domain"`
	Label  string `json:"label"`
	Tier   string `json:"tier"`
	URL    string `json:"url"`
}

type ClaimEvidence struct {
	Claim    string   `json:"claim"`
	Material bool     `json:"material"`
	Sources  []Source `json:"sources"`
}

type StorySummary struct {
	Confidence        string    `json:"confidence"`
	FirstSeenAt       time.Time `json:"firstSeenAt"`
	Headline          string    `json:"headline"`
	ID                string    `json:"id"`
	LastChangedAt     time.Time `json:"lastChangedAt"`
	PrimarySourceURL  string    `json:"primarySourceUrl"`
	ReadTimeMinutes   int       `json:"readTimeMinutes"`
	RecommendedAction string    `json:"recommendedAction"`
	Signal            string    `json:"signal"`
	SourceCount       int       `json:"sourceCount"`
	SourceTier        string    `json:"sourceTier"`
	Status            string    `json:"status"`
	Summary           string    `json:"summary"`
	WhyItMatters      string    `json:"whyItMatters"`
}

type StoryDetail struct {
	StorySummary
	Assertions        []ClaimEvidence `json:"assertions"`
	NormalizedContent string          `json:"normalizedContent"`
	Related           []StorySummary  `json:"related"`
	RevisionID        string          `json:"revisionId"`
	Sources           []Source        `json:"sources"`
	Uncertainties     []string        `json:"uncertainties"`
}

type TodayStats struct {
	CriticalAlerts   int    `json:"criticalAlerts"`
	EstimatedCostUSD string `json:"estimatedCostUsd"`
	Releases         int    `json:"releases"`
	ReviewRequired   int    `json:"reviewRequired"`
	SourceCoverage   int    `json:"sourceCoverage"`
}

type TodaySnapshot struct {
	CoverageEndAt   time.Time      `json:"coverageEndAt"`
	CoverageStartAt time.Time      `json:"coverageStartAt"`
	DeliveryState   string         `json:"deliveryState"`
	GeneratedAt     time.Time      `json:"generatedAt"`
	NextRunAt       *time.Time     `json:"nextRunAt"`
	Stats           TodayStats     `json:"stats"`
	Stories         []StorySummary `json:"stories"`
}

type LiveEvent struct {
	ID         string       `json:"id"`
	ObservedAt time.Time    `json:"observedAt"`
	Story      StorySummary `json:"story"`
	Type       string       `json:"type"`
}

type LiveSnapshot struct {
	Events      []LiveEvent `json:"events"`
	GeneratedAt time.Time   `json:"generatedAt"`
}

type Reader interface {
	Live(context.Context, time.Time) (LiveSnapshot, error)
	Story(context.Context, string) (StoryDetail, error)
	Today(context.Context, time.Time) (TodaySnapshot, error)
}
