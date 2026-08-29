package openaiwebhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/httpx"
	"github.com/traweezy/relantern/internal/jobqueue"
)

const MaximumEventBytes = 16 << 10

type VerifiedEvent struct {
	WebhookID      string    `json:"webhookId"`
	EventID        string    `json:"eventId"`
	EventType      string    `json:"eventType"`
	ResponseID     string    `json:"responseId"`
	EventCreatedAt time.Time `json:"eventCreatedAt"`
}

type AcceptResult struct {
	Accepted  bool `json:"accepted"`
	Duplicate bool `json:"duplicate"`
}

type Store struct {
	pool     *pgxpool.Pool
	inserter *jobqueue.Inserter
}

func NewStore(pool *pgxpool.Pool, inserter *jobqueue.Inserter) (*Store, error) {
	if pool == nil || inserter == nil {
		return nil, errors.New("OpenAI webhook store requires database and River inserter dependencies")
	}
	return &Store{pool: pool, inserter: inserter}, nil
}

func (store *Store) Accept(ctx context.Context, event VerifiedEvent, receivedAt time.Time) (AcceptResult, error) {
	if err := validateEvent(event, receivedAt); err != nil {
		return AcceptResult{}, err
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return AcceptResult{}, fmt.Errorf("begin verified OpenAI webhook: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var insertedWebhookID string
	err = transaction.QueryRow(ctx, `
		insert into app.openai_webhook_events (
			webhook_id, event_id, event_type, response_id, event_created_at, received_at
		) values ($1, $2, $3, $4, $5, $6)
		on conflict do nothing
		returning webhook_id`,
		event.WebhookID,
		event.EventID,
		event.EventType,
		event.ResponseID,
		event.EventCreatedAt,
		receivedAt,
	).Scan(&insertedWebhookID)
	if err == nil {
		if _, _, err := store.inserter.EnqueuePollOpenAIBackground(ctx, transaction, jobqueue.PollOpenAIBackgroundArgs{
			ResponseID: event.ResponseID,
			WebhookID:  event.WebhookID,
		}); err != nil {
			return AcceptResult{}, err
		}
		if err := transaction.Commit(ctx); err != nil {
			return AcceptResult{}, fmt.Errorf("commit verified OpenAI webhook: %w", err)
		}
		return AcceptResult{Accepted: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AcceptResult{}, fmt.Errorf("persist verified OpenAI webhook: %w", err)
	}
	var existing VerifiedEvent
	err = transaction.QueryRow(ctx, `
		select webhook_id, event_id, event_type, response_id, event_created_at
		from app.openai_webhook_events
		where webhook_id = $1 or event_id = $2
		order by webhook_id = $1 desc
		limit 1`, event.WebhookID, event.EventID).Scan(
		&existing.WebhookID,
		&existing.EventID,
		&existing.EventType,
		&existing.ResponseID,
		&existing.EventCreatedAt,
	)
	if err != nil {
		return AcceptResult{}, fmt.Errorf("select duplicate OpenAI webhook: %w", err)
	}
	if existing.WebhookID != event.WebhookID || existing.EventID != event.EventID ||
		existing.EventType != event.EventType || existing.ResponseID != event.ResponseID ||
		!existing.EventCreatedAt.Equal(event.EventCreatedAt) {
		return AcceptResult{}, errors.New("OpenAI webhook identifier collision does not match the persisted event")
	}
	if err := transaction.Commit(ctx); err != nil {
		return AcceptResult{}, fmt.Errorf("commit duplicate OpenAI webhook lookup: %w", err)
	}
	return AcceptResult{Accepted: true, Duplicate: true}, nil
}

func (store *Store) ReconcilePending(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1_000 {
		return 0, errors.New("background reconciliation limit must be between 1 and 1000")
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return 0, fmt.Errorf("begin background reconciliation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	rows, err := transaction.Query(ctx, `
		select run.id::text, run.provider_response_id, coalesce(event.webhook_id, '')
		from app.ai_runs run
		left join lateral (
			select webhook_id
			from app.openai_webhook_events
			where response_id = run.provider_response_id and processed_at is null
			order by received_at, webhook_id
			limit 1
		) event on true
		where run.background and run.state = 'running' and run.provider_response_id is not null
		order by run.updated_at, run.id
		limit $1
		for update of run skip locked`, limit)
	if err != nil {
		return 0, fmt.Errorf("select pending OpenAI responses: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var arguments jobqueue.PollOpenAIBackgroundArgs
		if err := rows.Scan(&arguments.RunID, &arguments.ResponseID, &arguments.WebhookID); err != nil {
			return 0, fmt.Errorf("scan pending OpenAI response: %w", err)
		}
		if _, inserted, err := store.inserter.EnqueuePollOpenAIBackground(ctx, transaction, arguments); err != nil {
			return 0, err
		} else if inserted {
			count++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate pending OpenAI responses: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit background reconciliation: %w", err)
	}
	return count, nil
}

func (store *Store) MarkProcessed(ctx context.Context, webhookID string, responseID string, processingError string, processedAt time.Time) error {
	if responseID == "" || processedAt.IsZero() || len(processingError) > 1_000 {
		return errors.New("webhook completion requires response ID, bounded error, and timestamp")
	}
	_, err := store.pool.Exec(ctx, `
		update app.openai_webhook_events
		set processed_at = $3, processing_error = nullif($4, '')
		where processed_at is null and response_id = $2
			and ($1::text = '' or webhook_id = $1)`, webhookID, responseID, processedAt, processingError)
	if err != nil {
		return fmt.Errorf("mark OpenAI webhook processed: %w", err)
	}
	return nil
}

type Handler struct {
	store  EventAcceptor
	token  string
	clock  func() time.Time
	logger *slog.Logger
}

type EventAcceptor interface {
	Accept(context.Context, VerifiedEvent, time.Time) (AcceptResult, error)
}

func NewHandler(store EventAcceptor, token string, logger *slog.Logger) (*Handler, error) {
	if store == nil || logger == nil || len(strings.TrimSpace(token)) < 32 {
		return nil, errors.New("private OpenAI webhook handler requires store, logger, and a strong service token")
	}
	return &Handler{store: store, token: token, clock: time.Now, logger: logger}, nil
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		httpx.WriteProblem(response, request, http.StatusMethodNotAllowed, "Method Not Allowed", "Only POST is accepted.")
		return
	}
	if !handler.authorized(request.Header.Get("Authorization")) {
		httpx.WriteProblem(response, request, http.StatusUnauthorized, "Unauthorized", "Private service authentication failed.")
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		httpx.WriteProblem(response, request, http.StatusUnsupportedMediaType, "Unsupported Media Type", "Content-Type must be application/json.")
		return
	}
	defer request.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, MaximumEventBytes))
	decoder.DisallowUnknownFields()
	var event VerifiedEvent
	if err := decoder.Decode(&event); err != nil {
		httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid Event", "The verified event body is invalid.")
		return
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid Event", "The verified event body must contain one JSON value.")
		return
	}
	result, err := handler.store.Accept(request.Context(), event, handler.clock().UTC())
	if err != nil {
		handler.logger.WarnContext(request.Context(), "verified OpenAI webhook rejected", "error", err)
		httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid Event", "The verified event failed durable validation.")
		return
	}
	httpx.WriteJSON(response, http.StatusAccepted, result)
}

func (handler *Handler) authorized(value string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	supplied := strings.TrimPrefix(value, prefix)
	return len(supplied) == len(handler.token) && subtle.ConstantTimeCompare([]byte(supplied), []byte(handler.token)) == 1
}

func validateEvent(event VerifiedEvent, receivedAt time.Time) error {
	if !bounded(event.WebhookID, 255) || !bounded(event.EventID, 255) || !bounded(event.ResponseID, 255) ||
		event.EventCreatedAt.IsZero() || receivedAt.IsZero() {
		return errors.New("verified OpenAI webhook identifiers and timestamps are required and bounded")
	}
	switch event.EventType {
	case "response.completed", "response.failed", "response.incomplete", "response.cancelled":
	default:
		return fmt.Errorf("unsupported OpenAI webhook event type %q", event.EventType)
	}
	if event.EventCreatedAt.After(receivedAt.Add(10 * time.Minute)) {
		return errors.New("OpenAI webhook event timestamp is implausibly in the future")
	}
	return nil
}

func bounded(value string, maximum int) bool {
	length := len(strings.TrimSpace(value))
	return length > 0 && length <= maximum
}
