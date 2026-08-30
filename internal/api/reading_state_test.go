package api_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/api"
	"github.com/traweezy/relantern/internal/readingstate"
)

const readingUserID = "01991234-5678-7abc-8def-0123456789ac"

type fixtureReadingRepository struct {
	command  readingstate.Command
	err      error
	feedback readingstate.FeedbackCommand
}

func (repository *fixtureReadingRepository) Annotations(context.Context, string, string) ([]readingstate.Annotation, error) {
	return []readingstate.Annotation{}, repository.err
}

func (repository *fixtureReadingRepository) BulkMutate(_ context.Context, command readingstate.BulkCommand) (readingstate.BulkMutationResult, error) {
	repository.command.UserID = command.UserID
	return fixtureBulkResult(command.UserID), repository.err
}

func (repository *fixtureReadingRepository) Collection(context.Context, readingstate.CollectionQuery) (readingstate.Collection, error) {
	return readingstate.Collection{Items: []readingstate.StoryListItem{}}, repository.err
}

func (repository *fixtureReadingRepository) CreateAnnotation(context.Context, string, string, readingstate.AnnotationInput) (readingstate.Annotation, error) {
	return readingstate.Annotation{}, repository.err
}

func (repository *fixtureReadingRepository) CreateTag(context.Context, string, readingstate.TagInput) (readingstate.Tag, error) {
	return readingstate.Tag{}, repository.err
}

func (repository *fixtureReadingRepository) DeleteAnnotation(context.Context, string, string) error {
	return repository.err
}

func (repository *fixtureReadingRepository) DeleteTag(context.Context, string, string) error {
	return repository.err
}

func (repository *fixtureReadingRepository) Mutate(_ context.Context, command readingstate.Command) (readingstate.MutationResult, error) {
	repository.command = command
	return fixtureMutation(command.StoryID), repository.err
}

func (repository *fixtureReadingRepository) RecordFeedback(_ context.Context, command readingstate.FeedbackCommand) (readingstate.Feedback, error) {
	repository.feedback = command
	return readingstate.Feedback{
		CreatedAt: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC),
		ID:        "01991234-5678-7abc-8def-0123456789af",
		StoryID:   command.StoryID,
		Type:      command.Type,
	}, repository.err
}

func (repository *fixtureReadingRepository) ReorderLater(_ context.Context, command readingstate.LaterOrderCommand) (readingstate.BulkMutationResult, error) {
	return fixtureBulkResult(command.UserID), repository.err
}

func (repository *fixtureReadingRepository) State(_ context.Context, _ string, requestedStoryID string) (readingstate.StoryState, error) {
	return fixtureState(requestedStoryID), repository.err
}

func (repository *fixtureReadingRepository) States(_ context.Context, _ string, requestedStoryIDs []string) (readingstate.StoryStates, error) {
	states := make([]readingstate.StoryState, 0, len(requestedStoryIDs))
	for _, requestedStoryID := range requestedStoryIDs {
		states = append(states, fixtureState(requestedStoryID))
	}
	return readingstate.StoryStates{States: states}, repository.err
}

func (repository *fixtureReadingRepository) Tags(context.Context, string) ([]readingstate.Tag, error) {
	return []readingstate.Tag{}, repository.err
}

func (repository *fixtureReadingRepository) Undo(context.Context, string, string, string) (readingstate.MutationResult, error) {
	return fixtureMutation(storyID), repository.err
}

func (repository *fixtureReadingRepository) UndoBulk(context.Context, string, string) (readingstate.BulkMutationResult, error) {
	return fixtureBulkResult(readingUserID), repository.err
}

func (repository *fixtureReadingRepository) UpdateAnnotation(context.Context, string, string, readingstate.AnnotationInput) (readingstate.Annotation, error) {
	return readingstate.Annotation{}, repository.err
}

func (repository *fixtureReadingRepository) UpdateTag(context.Context, string, string, readingstate.TagInput) (readingstate.Tag, error) {
	return readingstate.Tag{}, repository.err
}

func fixtureState(requestedStoryID string) readingstate.StoryState {
	return readingstate.StoryState{
		Location:  readingstate.LocationInbox,
		StoryID:   requestedStoryID,
		TagIDs:    []string{},
		UpdatedAt: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC),
	}
}

func fixtureMutation(requestedStoryID string) readingstate.MutationResult {
	return readingstate.MutationResult{
		MutationID:   "01991234-5678-7abc-8def-0123456789ad",
		State:        fixtureState(requestedStoryID),
		UndoDeadline: time.Date(2026, time.August, 29, 12, 0, 10, 0, time.UTC),
	}
}

func fixtureBulkResult(_ string) readingstate.BulkMutationResult {
	mutation := fixtureMutation(storyID)
	return readingstate.BulkMutationResult{
		AffectedCount: 1,
		BulkID:        "01991234-5678-7abc-8def-0123456789ae",
		Mutations:     []readingstate.MutationResult{mutation},
		UndoDeadline:  mutation.UndoDeadline,
	}
}

func newReadingStateApplication(repository readingstate.Repository, token string) api.Application {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return api.New(
		logger,
		api.Info{Version: "test"},
		func(context.Context) error { return nil },
		api.WithReadingState(repository, token),
	)
}

func TestReadingStateEndpointsRequireServiceCredential(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	application := newReadingStateApplication(&fixtureReadingRepository{}, token)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/inbox", nil)
	request.Header.Set("X-Relantern-User-ID", readingUserID)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestReadingStateMutationPropagatesOwnerAndStory(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	repository := &fixtureReadingRepository{}
	application := newReadingStateApplication(repository, token)
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/stories/"+storyID+"/state",
		strings.NewReader(`{"action":"mark_read","version":0,"idempotencyKey":"0123456789abcdef"}`),
	)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Relantern-User-ID", readingUserID)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.command.UserID != readingUserID || repository.command.StoryID != storyID || repository.command.Action != readingstate.ActionMarkRead {
		t.Fatalf("propagated command = %+v", repository.command)
	}
}

func TestReadingStateConflictReturnsProblem(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	application := newReadingStateApplication(&fixtureReadingRepository{err: readingstate.ErrConflict}, token)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/stories/"+storyID+"/state", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Relantern-User-ID", readingUserID)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusConflict, response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}
}

func TestStoryFeedbackPropagatesOwnerAndStory(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	repository := &fixtureReadingRepository{}
	application := newReadingStateApplication(repository, token)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/stories/"+storyID+"/feedback",
		strings.NewReader(`{"type":"useful","idempotencyKey":"0123456789abcdef"}`),
	)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Relantern-User-ID", readingUserID)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.feedback.UserID != readingUserID || repository.feedback.StoryID != storyID || repository.feedback.Type != readingstate.FeedbackUseful {
		t.Fatalf("propagated feedback = %+v", repository.feedback)
	}
}

func TestReadingStateUnexpectedErrorDoesNotLeakDetails(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	secretError := errors.New("database password is secret")
	application := newReadingStateApplication(&fixtureReadingRepository{err: secretError}, token)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/stories/"+storyID+"/state", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Relantern-User-ID", readingUserID)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), secretError.Error()) {
		t.Fatal("problem response leaked an internal error")
	}
}
