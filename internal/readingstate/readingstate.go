package readingstate

import (
	"context"
	"errors"
	"time"

	"github.com/traweezy/relantern/internal/intelligence"
)

var (
	ErrConflict        = errors.New("reading state version conflict")
	ErrInvalid         = errors.New("invalid reading state command")
	ErrNotFound        = errors.New("reading state target not found")
	ErrUndoConflict    = errors.New("a newer reading state mutation prevents undo")
	ErrUndoExpired     = errors.New("reading state undo deadline expired")
	ErrUndoUnavailable = errors.New("reading state mutation cannot be undone")
)

type Location string

const (
	LocationInbox   Location = "inbox"
	LocationLater   Location = "later"
	LocationArchive Location = "archive"
)

type Action string

const (
	ActionAddTag         Action = "add_tag"
	ActionAlreadyKnown   Action = "already_known"
	ActionArchive        Action = "archive"
	ActionDismiss        Action = "dismiss"
	ActionMarkRead       Action = "mark_read"
	ActionMarkUnread     Action = "mark_unread"
	ActionMoveInbox      Action = "move_inbox"
	ActionMoveLater      Action = "move_later"
	ActionRemoveTag      Action = "remove_tag"
	ActionReorderLater   Action = "reorder_later"
	ActionReturnSnoozed  Action = "return_snoozed"
	ActionSnooze         Action = "snooze"
	ActionStar           Action = "star"
	ActionUnstar         Action = "unstar"
	ActionUnsnooze       Action = "unsnooze"
	ActionUpdateProgress Action = "update_progress"
)

type StoryState struct {
	DismissedReason     *string    `json:"dismissedReason"`
	IsRead              bool       `json:"isRead"`
	LastParagraphID     *string    `json:"lastParagraphId"`
	LaterPosition       *int64     `json:"laterPosition"`
	Location            Location   `json:"location"`
	ReadAt              *time.Time `json:"readAt"`
	ReadingProgress     float64    `json:"readingProgress"`
	SnoozedFromLocation *Location  `json:"snoozedFromLocation"`
	SnoozedUntil        *time.Time `json:"snoozedUntil"`
	StarredAt           *time.Time `json:"starredAt"`
	StoryID             string     `json:"storyId"`
	TagIDs              []string   `json:"tagIds"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	Version             int64      `json:"version"`
}

type Command struct {
	Action          Action     `json:"action"`
	DismissedReason *string    `json:"dismissedReason,omitempty"`
	ExpectedVersion int64      `json:"version"`
	IdempotencyKey  string     `json:"idempotencyKey"`
	LastParagraphID *string    `json:"lastParagraphId,omitempty"`
	LaterPosition   *int64     `json:"-"`
	ReadingProgress *float64   `json:"readingProgress,omitempty"`
	SnoozedUntil    *time.Time `json:"snoozedUntil,omitempty"`
	StoryID         string     `json:"-"`
	TagID           *string    `json:"tagId,omitempty"`
	UserID          string     `json:"-"`
}

type BulkItem struct {
	StoryID string `json:"storyId"`
	Version int64  `json:"version"`
}

type BulkCommand struct {
	Action          Action     `json:"action"`
	DismissedReason *string    `json:"dismissedReason,omitempty"`
	ExpectedCount   int        `json:"expectedCount"`
	IdempotencyKey  string     `json:"idempotencyKey"`
	Items           []BulkItem `json:"items"`
	SnoozedUntil    *time.Time `json:"snoozedUntil,omitempty"`
	TagID           *string    `json:"tagId,omitempty"`
	UserID          string     `json:"-"`
}

type LaterOrderCommand struct {
	ExpectedCount  int        `json:"expectedCount"`
	IdempotencyKey string     `json:"idempotencyKey"`
	Items          []BulkItem `json:"items"`
	UserID         string     `json:"-"`
}

type MutationResult struct {
	MutationID   string     `json:"mutationId"`
	State        StoryState `json:"state"`
	UndoDeadline time.Time  `json:"undoDeadline"`
}

type BulkMutationResult struct {
	AffectedCount int              `json:"affectedCount"`
	BulkID        string           `json:"bulkId"`
	Mutations     []MutationResult `json:"mutations"`
	UndoDeadline  time.Time        `json:"undoDeadline"`
}

type CollectionKind string

const (
	CollectionArchive CollectionKind = "archive"
	CollectionInbox   CollectionKind = "inbox"
	CollectionLater   CollectionKind = "later"
	CollectionSnoozed CollectionKind = "snoozed"
	CollectionStarred CollectionKind = "starred"
)

type CollectionQuery struct {
	Cursor string
	Kind   CollectionKind
	Limit  int
	Now    time.Time
	UserID string
}

type StoryListItem struct {
	State StoryState                `json:"state"`
	Story intelligence.StorySummary `json:"story"`
}

type Collection struct {
	Items      []StoryListItem `json:"items"`
	NextCursor *string         `json:"nextCursor"`
	Total      int             `json:"total"`
}

type StoryStates struct {
	States []StoryState `json:"states"`
}

type Tag struct {
	ColorToken string    `json:"colorToken"`
	CreatedAt  time.Time `json:"createdAt"`
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type TagInput struct {
	ColorToken string `json:"colorToken"`
	Name       string `json:"name"`
}

type FeedbackType string

const (
	FeedbackUseful     FeedbackType = "useful"
	FeedbackKnown      FeedbackType = "already_known"
	FeedbackIrrelevant FeedbackType = "irrelevant"
	FeedbackShallow    FeedbackType = "too_shallow"
	FeedbackVerbose    FeedbackType = "too_verbose"
	FeedbackIncorrect  FeedbackType = "incorrect"
)

type FeedbackCommand struct {
	IdempotencyKey string       `json:"idempotencyKey"`
	Note           *string      `json:"note,omitempty"`
	StoryID        string       `json:"-"`
	Type           FeedbackType `json:"type"`
	UserID         string       `json:"-"`
}

type Feedback struct {
	CreatedAt time.Time    `json:"createdAt"`
	ID        string       `json:"id"`
	Note      *string      `json:"note"`
	StoryID   string       `json:"storyId"`
	Type      FeedbackType `json:"type"`
}

type AnnotationType string

const (
	AnnotationDocumentNote  AnnotationType = "document_note"
	AnnotationHighlight     AnnotationType = "highlight"
	AnnotationHighlightNote AnnotationType = "highlight_note"
)

type Annotation struct {
	Body        string         `json:"body"`
	CreatedAt   time.Time      `json:"createdAt"`
	EndOffset   *int           `json:"endOffset"`
	ID          string         `json:"id"`
	Orphaned    bool           `json:"orphaned"`
	QuoteHash   *string        `json:"quoteHash"`
	RevisionID  string         `json:"revisionId"`
	StartOffset *int           `json:"startOffset"`
	StoryID     string         `json:"storyId"`
	Type        AnnotationType `json:"type"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type AnnotationInput struct {
	Body        string         `json:"body"`
	EndOffset   *int           `json:"endOffset,omitempty"`
	QuoteHash   *string        `json:"quoteHash,omitempty"`
	RevisionID  *string        `json:"revisionId,omitempty"`
	StartOffset *int           `json:"startOffset,omitempty"`
	Type        AnnotationType `json:"type"`
}

type Repository interface {
	Annotations(context.Context, string, string) ([]Annotation, error)
	BulkMutate(context.Context, BulkCommand) (BulkMutationResult, error)
	Collection(context.Context, CollectionQuery) (Collection, error)
	CreateAnnotation(context.Context, string, string, AnnotationInput) (Annotation, error)
	CreateTag(context.Context, string, TagInput) (Tag, error)
	DeleteAnnotation(context.Context, string, string) error
	DeleteTag(context.Context, string, string) error
	Mutate(context.Context, Command) (MutationResult, error)
	RecordFeedback(context.Context, FeedbackCommand) (Feedback, error)
	ReorderLater(context.Context, LaterOrderCommand) (BulkMutationResult, error)
	State(context.Context, string, string) (StoryState, error)
	States(context.Context, string, []string) (StoryStates, error)
	Tags(context.Context, string) ([]Tag, error)
	Undo(context.Context, string, string, string) (MutationResult, error)
	UndoBulk(context.Context, string, string) (BulkMutationResult, error)
	UpdateAnnotation(context.Context, string, string, AnnotationInput) (Annotation, error)
	UpdateTag(context.Context, string, string, TagInput) (Tag, error)
}
