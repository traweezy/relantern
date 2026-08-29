package pgstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/storage"
)

type transactionStarter interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type Store struct {
	database transactionStarter
}

func New(database transactionStarter) (*Store, error) {
	if database == nil {
		return nil, errors.New("parser PostgreSQL store requires a database")
	}
	return &Store{database: database}, nil
}

func (store *Store) RecordSuccess(ctx context.Context, request parsing.RecordRequest) (parsing.RecordResult, error) {
	if strings.TrimSpace(request.RawDocumentID) == "" || strings.TrimSpace(request.ObjectKey) == "" {
		return parsing.RecordResult{}, errors.New("raw document id and normalized object key are required")
	}
	if request.Result.ParserName == "" || request.Result.ParserVersion == "" || request.Result.NormalizedText == "" {
		return parsing.RecordResult{}, errors.New("complete parser metadata and normalized text are required")
	}
	if request.AttemptedAt.IsZero() || request.CompletedAt.IsZero() || request.CompletedAt.Before(request.AttemptedAt) {
		return parsing.RecordResult{}, errors.New("valid parser attempt timestamps are required")
	}
	if err := parsing.ValidateResult(request.Result); err != nil {
		return parsing.RecordResult{}, fmt.Errorf("validate parser result: %w", err)
	}
	if sha256.Sum256([]byte(request.Result.NormalizedText)) != request.Result.NormalizedSHA256 {
		return parsing.RecordResult{}, errors.New("normalized digest does not match normalized text")
	}
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return parsing.RecordResult{}, fmt.Errorf("begin parser transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	sourceID, canonicalURL, err := rawDocumentIdentity(ctx, transaction, request.RawDocumentID)
	if err != nil {
		return parsing.RecordResult{}, err
	}
	expectedObjectKey, err := storage.NormalizedObjectKey(sourceID, request.Result.NormalizedSHA256)
	if err != nil {
		return parsing.RecordResult{}, fmt.Errorf("derive normalized object key: %w", err)
	}
	if request.ObjectKey != expectedObjectKey {
		return parsing.RecordResult{}, fmt.Errorf("normalized object key %q does not match expected key %q", request.ObjectKey, expectedObjectKey)
	}
	if _, err := transaction.Exec(ctx, `
		select pg_advisory_xact_lock(
			hashtextextended(jsonb_build_array($1::text, $2::text)::text, 0)
		)`, sourceID, canonicalURL); err != nil {
		return parsing.RecordResult{}, fmt.Errorf("lock revision identity: %w", err)
	}
	existingID, existing, err := findExistingRevision(ctx, transaction, request.RawDocumentID, request.Result.NormalizedSHA256[:])
	if err != nil {
		return parsing.RecordResult{}, err
	}
	if existing {
		if err := recordAttempt(ctx, transaction, request.RawDocumentID, existingID, "unchanged", request.Result.ParserName, request.Result.ParserVersion, request.AttemptedAt, request.CompletedAt, "", request.Result.Warnings); err != nil {
			return parsing.RecordResult{}, err
		}
		if err := transaction.Commit(ctx); err != nil {
			return parsing.RecordResult{}, fmt.Errorf("commit unchanged parser result: %w", err)
		}
		return parsing.RecordResult{RevisionID: existingID, Outcome: "unchanged", ChangeReason: "normalized SHA-256 is unchanged"}, nil
	}

	previous, err := findPreviousRevision(ctx, transaction, sourceID, canonicalURL)
	if err != nil {
		return parsing.RecordResult{}, err
	}
	changeKind := "initial"
	changeReason := "first normalized revision for canonical source document"
	materialChange := false
	var previousRevisionID *string
	if previous.ID != "" {
		changeKind = "material"
		changeReason = describeMaterialChange(previous, request.Result)
		materialChange = true
		previousRevisionID = &previous.ID
	}
	outline, err := json.Marshal(request.Result.Outline)
	if err != nil {
		return parsing.RecordResult{}, fmt.Errorf("encode revision outline: %w", err)
	}
	offsetMap, err := json.Marshal(request.Result.OffsetMap)
	if err != nil {
		return parsing.RecordResult{}, fmt.Errorf("encode revision offset map: %w", err)
	}
	warnings, err := json.Marshal(request.Result.Warnings)
	if request.Result.Warnings == nil {
		warnings = []byte("[]")
	}
	if err != nil {
		return parsing.RecordResult{}, fmt.Errorf("encode revision warnings: %w", err)
	}
	var revisionID string
	err = transaction.QueryRow(ctx, `
		insert into app.content_revisions (
			raw_document_id, previous_revision_id, normalized_sha256,
			normalized_text_object_key, parser_name, parser_version,
			title, author, language, source_published_at, source_updated_at,
			normalized_bytes, outline, offset_map, warnings, change_kind,
			change_reason, material_change, observed_at
		) values (
			$1::uuid, $2::uuid, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18, $19
		) returning id::text`,
		request.RawDocumentID,
		previousRevisionID,
		request.Result.NormalizedSHA256[:],
		request.ObjectKey,
		request.Result.ParserName,
		request.Result.ParserVersion,
		request.Result.Title,
		request.Result.Author,
		request.Result.Language,
		nullableTime(request.Result.SourcePublishedAt),
		nullableTime(request.Result.SourceUpdatedAt),
		int64(len(request.Result.NormalizedText)),
		outline,
		offsetMap,
		warnings,
		changeKind,
		changeReason,
		materialChange,
		request.CompletedAt,
	).Scan(&revisionID)
	if err != nil {
		return parsing.RecordResult{}, fmt.Errorf("insert normalized revision: %w", err)
	}
	if err := recordAttempt(ctx, transaction, request.RawDocumentID, revisionID, "created", request.Result.ParserName, request.Result.ParserVersion, request.AttemptedAt, request.CompletedAt, "", request.Result.Warnings); err != nil {
		return parsing.RecordResult{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return parsing.RecordResult{}, fmt.Errorf("commit normalized revision: %w", err)
	}
	return parsing.RecordResult{
		RevisionID:         revisionID,
		PreviousRevisionID: valueOrEmpty(previousRevisionID),
		Outcome:            "created",
		MaterialChange:     materialChange,
		ChangeReason:       changeReason,
	}, nil
}

func (store *Store) RecordFailure(ctx context.Context, request parsing.FailureRequest) error {
	if strings.TrimSpace(request.RawDocumentID) == "" || strings.TrimSpace(request.ParserName) == "" || request.ErrorCode == "" {
		return errors.New("raw document id, parser name, and parser error code are required")
	}
	if request.AttemptedAt.IsZero() || request.CompletedAt.IsZero() || request.CompletedAt.Before(request.AttemptedAt) {
		return errors.New("valid parser attempt timestamps are required")
	}
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin parser failure transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, _, err := rawDocumentIdentity(ctx, transaction, request.RawDocumentID); err != nil {
		return err
	}
	if err := recordAttempt(ctx, transaction, request.RawDocumentID, "", "failed", request.ParserName, parsing.ParserVersion, request.AttemptedAt, request.CompletedAt, string(request.ErrorCode), request.Warnings); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit parser failure: %w", err)
	}
	return nil
}

func rawDocumentIdentity(ctx context.Context, transaction pgx.Tx, rawDocumentID string) (string, string, error) {
	var sourceID string
	var canonicalURL string
	err := transaction.QueryRow(ctx, `
		select source_id, canonical_url
		from app.raw_documents
		where id = $1::uuid`, rawDocumentID).Scan(&sourceID, &canonicalURL)
	if err != nil {
		return "", "", fmt.Errorf("select raw document %q: %w", rawDocumentID, err)
	}
	return sourceID, canonicalURL, nil
}

func findExistingRevision(ctx context.Context, transaction pgx.Tx, rawDocumentID string, digest []byte) (string, bool, error) {
	var revisionID string
	err := transaction.QueryRow(ctx, `
		select id::text
		from app.content_revisions
		where raw_document_id = $1::uuid
		  and normalized_sha256 = $2
		order by observed_at desc, id desc
		limit 1`, rawDocumentID, digest).Scan(&revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find existing normalized revision: %w", err)
	}
	return revisionID, true, nil
}

type previousRevision struct {
	ID              string
	Title           string
	NormalizedBytes int64
}

func findPreviousRevision(ctx context.Context, transaction pgx.Tx, sourceID string, canonicalURL string) (previousRevision, error) {
	var previous previousRevision
	err := transaction.QueryRow(ctx, `
		select revision.id::text, revision.title, revision.normalized_bytes
		from app.content_revisions revision
		join app.raw_documents raw on raw.id = revision.raw_document_id
		where raw.source_id = $1 and raw.canonical_url = $2
		order by revision.observed_at desc, revision.id desc
		limit 1`, sourceID, canonicalURL).Scan(&previous.ID, &previous.Title, &previous.NormalizedBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return previousRevision{}, nil
	}
	if err != nil {
		return previousRevision{}, fmt.Errorf("find previous normalized revision: %w", err)
	}
	return previous, nil
}

func describeMaterialChange(previous previousRevision, current parsing.Result) string {
	changes := []string{"normalized SHA-256 changed"}
	if previous.Title != current.Title {
		changes = append(changes, "title changed")
	}
	currentBytes := int64(len(current.NormalizedText))
	if previous.NormalizedBytes != currentBytes {
		changes = append(changes, fmt.Sprintf("normalized bytes changed from %d to %d", previous.NormalizedBytes, currentBytes))
	}
	return strings.Join(changes, "; ")
}

func recordAttempt(
	ctx context.Context,
	transaction pgx.Tx,
	rawDocumentID string,
	revisionID string,
	outcome string,
	parserName string,
	parserVersion string,
	attemptedAt time.Time,
	completedAt time.Time,
	errorCode string,
	parserWarnings []parsing.Warning,
) error {
	if parserWarnings == nil {
		parserWarnings = []parsing.Warning{}
	}
	warnings, err := json.Marshal(parserWarnings)
	if err != nil {
		return fmt.Errorf("encode parse-attempt warnings: %w", err)
	}
	var revisionValue *string
	if revisionID != "" {
		revisionValue = &revisionID
	}
	_, err = transaction.Exec(ctx, `
		insert into app.source_parse_attempts (
			raw_document_id, revision_id, outcome, parser_name, parser_version,
			attempted_at, completed_at, duration_ms, error_code, warnings
		) values (
			$1::uuid, $2::uuid, $3, $4, $5,
			$6, $7, $8, nullif($9, ''), $10
		)`,
		rawDocumentID,
		revisionValue,
		outcome,
		parserName,
		parserVersion,
		attemptedAt,
		completedAt,
		durationMilliseconds(attemptedAt, completedAt),
		errorCode,
		warnings,
	)
	if err != nil {
		return fmt.Errorf("record parser attempt: %w", err)
	}
	return nil
}

func durationMilliseconds(start time.Time, end time.Time) int32 {
	duration := end.Sub(start).Milliseconds()
	if duration < 0 {
		return 0
	}
	if duration > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(duration)
}

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
