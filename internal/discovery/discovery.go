package discovery

import (
	"context"
	"errors"
	"time"

	"github.com/traweezy/relantern/internal/embedding"
)

const (
	MaximumOPMLBytes      = 2 << 20
	MaximumExportStoryIDs = 100
)

var (
	ErrConflict = errors.New("discovery state conflict")
	ErrInvalid  = errors.New("invalid discovery request")
	ErrNotFound = errors.New("discovery resource not found")
)

type SearchRequest struct {
	UserID         string
	Query          string
	Topic          string
	SourceTier     string
	LifecycleState string
	RadarState     string
	Action         string
	Saved          *bool
	After          *time.Time
	Before         *time.Time
	Limit          int
}

type SearchExplanation struct {
	KeywordRank        *int64   `json:"keywordRank"`
	SemanticRank       *int64   `json:"semanticRank"`
	SemanticSimilarity *float64 `json:"semanticSimilarity"`
	Summary            string   `json:"summary"`
}

type SearchResult struct {
	StoryID           string            `json:"storyId"`
	ItemID            string            `json:"itemId"`
	Title             string            `json:"title"`
	Summary           string            `json:"summary"`
	PackageName       string            `json:"packageName"`
	SourceTier        string            `json:"sourceTier"`
	LifecycleState    string            `json:"lifecycleState"`
	Signal            string            `json:"signal"`
	RecommendedAction string            `json:"recommendedAction"`
	FirstSeenAt       time.Time         `json:"firstSeenAt"`
	Saved             bool              `json:"saved"`
	Score             float64           `json:"score"`
	Explanation       SearchExplanation `json:"explanation"`
}

type SearchResponse struct {
	Query       string         `json:"query"`
	Results     []SearchResult `json:"results"`
	ResultCount int            `json:"resultCount"`
	Explanation string         `json:"explanation"`
}

type SearchFilters struct {
	Topic          string `json:"topic,omitempty"`
	SourceTier     string `json:"sourceTier,omitempty"`
	LifecycleState string `json:"lifecycle,omitempty"`
	RadarState     string `json:"radar,omitempty"`
	Action         string `json:"action,omitempty"`
	Saved          string `json:"saved,omitempty"`
	After          string `json:"after,omitempty"`
	Before         string `json:"before,omitempty"`
}

type SavedSearch struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Query     string        `json:"query"`
	Filters   SearchFilters `json:"filters"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type SaveSearchRequest struct {
	UserID  string        `json:"-"`
	Name    string        `json:"name"`
	Query   string        `json:"query"`
	Filters SearchFilters `json:"filters"`
}

type ReleaseEntry struct {
	StoryID        string     `json:"storyId"`
	Technology     string     `json:"technology"`
	PackageName    string     `json:"packageName"`
	Version        string     `json:"version"`
	State          string     `json:"state"`
	Headline       string     `json:"headline"`
	Summary        string     `json:"summary"`
	SourceTier     string     `json:"sourceTier"`
	SourceURL      string     `json:"sourceUrl"`
	TargetDate     *time.Time `json:"targetDate"`
	LastVerifiedAt time.Time  `json:"lastVerifiedAt"`
}

type TechnologyRelease struct {
	Technology     string         `json:"technology"`
	PackageName    string         `json:"packageName"`
	CurrentVersion string         `json:"currentVersion"`
	NewestVersion  string         `json:"newestVersion"`
	UpgradeStatus  string         `json:"upgradeStatus"`
	Entries        []ReleaseEntry `json:"entries"`
}

type ReleaseCatalog struct {
	GeneratedAt  time.Time           `json:"generatedAt"`
	Technologies []TechnologyRelease `json:"technologies"`
	ComingSoon   []ReleaseEntry      `json:"comingSoon"`
}

type ManualCaptureRequest struct {
	UserID         string
	URL            string `json:"url"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type ManualCapture struct {
	ID          string     `json:"id"`
	URL         string     `json:"url"`
	State       string     `json:"state"`
	StoryID     string     `json:"storyId,omitempty"`
	ErrorCode   string     `json:"errorCode,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type ImportCandidate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Connector   string `json:"connector"`
	Duplicate   bool   `json:"duplicate"`
	Valid       bool   `json:"valid"`
	Explanation string `json:"explanation"`
}

type ImportPreview struct {
	ID         string            `json:"id"`
	Candidates []ImportCandidate `json:"candidates"`
	ExpiresAt  time.Time         `json:"expiresAt"`
}

type ImportCommitRequest struct {
	UserID      string
	PreviewID   string   `json:"previewId"`
	ApprovedIDs []string `json:"approvedIds"`
}

type ImportCommitResult struct {
	ImportedCount    int      `json:"importedCount"`
	PendingSourceIDs []string `json:"pendingSourceIds"`
}

type MetadataExportRequest struct {
	UserID string
	Format string
}

type MarkdownExportRequest struct {
	UserID   string
	StoryIDs []string `json:"storyIds"`
}

type Export struct {
	ContentType string
	Filename    string
	Payload     []byte
}

type Store interface {
	Search(context.Context, SearchRequest, embedding.Vector) (SearchResponse, error)
	ListSavedSearches(context.Context, string) ([]SavedSearch, error)
	SaveSearch(context.Context, SaveSearchRequest, time.Time) (SavedSearch, error)
	DeleteSavedSearch(context.Context, string, string) error
	Releases(context.Context, string, time.Time) (ReleaseCatalog, error)
	ImportURL(context.Context, ManualCaptureRequest) (ManualCapture, error)
	PreviewOPML(context.Context, string, []byte, time.Time) (ImportPreview, error)
	CommitOPML(context.Context, ImportCommitRequest, time.Time) (ImportCommitResult, error)
	ExportOPML(context.Context, string) (Export, error)
	ExportMetadata(context.Context, MetadataExportRequest) (Export, error)
	ExportMarkdown(context.Context, MarkdownExportRequest) (Export, error)
}

type Embedder interface {
	Embed(context.Context, string) (embedding.Vector, error)
}

type Service struct {
	store    Store
	embedder Embedder
}

func NewService(store Store, embedder Embedder) (*Service, error) {
	if store == nil || embedder == nil {
		return nil, errors.New("discovery service requires storage and an embedder")
	}
	return &Service{store: store, embedder: embedder}, nil
}

func (service *Service) Search(ctx context.Context, request SearchRequest) (SearchResponse, error) {
	vector, err := service.embedder.Embed(ctx, request.Query)
	if err != nil {
		return SearchResponse{}, err
	}
	return service.store.Search(ctx, request, vector)
}

func (service *Service) ListSavedSearches(ctx context.Context, userID string) ([]SavedSearch, error) {
	return service.store.ListSavedSearches(ctx, userID)
}

func (service *Service) SaveSearch(ctx context.Context, request SaveSearchRequest, now time.Time) (SavedSearch, error) {
	return service.store.SaveSearch(ctx, request, now)
}

func (service *Service) DeleteSavedSearch(ctx context.Context, userID string, savedSearchID string) error {
	return service.store.DeleteSavedSearch(ctx, userID, savedSearchID)
}

func (service *Service) Releases(ctx context.Context, userID string, now time.Time) (ReleaseCatalog, error) {
	return service.store.Releases(ctx, userID, now)
}

func (service *Service) ImportURL(ctx context.Context, request ManualCaptureRequest) (ManualCapture, error) {
	return service.store.ImportURL(ctx, request)
}

func (service *Service) PreviewOPML(ctx context.Context, userID string, document []byte, now time.Time) (ImportPreview, error) {
	return service.store.PreviewOPML(ctx, userID, document, now)
}

func (service *Service) CommitOPML(ctx context.Context, request ImportCommitRequest, now time.Time) (ImportCommitResult, error) {
	return service.store.CommitOPML(ctx, request, now)
}

func (service *Service) ExportOPML(ctx context.Context, userID string) (Export, error) {
	return service.store.ExportOPML(ctx, userID)
}

func (service *Service) ExportMetadata(ctx context.Context, request MetadataExportRequest) (Export, error) {
	return service.store.ExportMetadata(ctx, request)
}

func (service *Service) ExportMarkdown(ctx context.Context, request MarkdownExportRequest) (Export, error) {
	return service.store.ExportMarkdown(ctx, request)
}
