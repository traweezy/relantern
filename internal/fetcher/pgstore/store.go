package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/fetcher"
)

type Store struct{}

func (Store) Record(
	ctx context.Context,
	transaction pgx.Tx,
	registryID string,
	sourceID string,
	contentPolicy string,
	result fetcher.Result,
) error {
	if transaction == nil {
		return errors.New("fetch record transaction is required")
	}
	if registryID == "" || sourceID == "" || len(result.Attempts) == 0 {
		return errors.New("registry id, source id, and at least one attempt are required")
	}
	var endpointID string
	if err := transaction.QueryRow(ctx, `
		select id::text
		from app.source_endpoints
		where registry_id = $1
		for update`, registryID).Scan(&endpointID); err != nil {
		return fmt.Errorf("select endpoint %q: %w", registryID, err)
	}
	for index, attempt := range result.Attempts {
		isFinal := index == len(result.Attempts)-1
		if err := recordAttempt(ctx, transaction, endpointID, result, attempt, isFinal); err != nil {
			return err
		}
	}
	finalAttempt := result.Attempts[len(result.Attempts)-1]
	if err := updateEndpoint(ctx, transaction, endpointID, result.Outcome, finalAttempt); err != nil {
		return err
	}
	if result.Outcome == fetcher.OutcomeStored || result.Outcome == fetcher.OutcomeMetadataOnly || result.Outcome == fetcher.OutcomeNotModified {
		if err := upsertCheckpoint(ctx, transaction, endpointID, result); err != nil {
			return err
		}
	}
	if result.Outcome == fetcher.OutcomeStored || result.Outcome == fetcher.OutcomeMetadataOnly {
		if finalAttempt.FinalURL == "" {
			return errors.New("successful fetch requires a final URL")
		}
		if err := recordRawDocument(ctx, transaction, sourceID, contentPolicy, result, finalAttempt); err != nil {
			return err
		}
	}
	return nil
}

func recordAttempt(ctx context.Context, transaction pgx.Tx, endpointID string, result fetcher.Result, attempt fetcher.Attempt, final bool) error {
	outcome := fetcher.OutcomeFailed
	errorCode := string(attempt.ErrorCode)
	var digest []byte
	var objectKey *string
	if final {
		outcome = result.Outcome
		switch outcome {
		case fetcher.OutcomeStored:
			digest = result.SHA256[:]
			objectKey = &result.ObjectKey
			errorCode = ""
		case fetcher.OutcomeMetadataOnly:
			digest = result.SHA256[:]
			errorCode = ""
		case fetcher.OutcomeNotModified:
			errorCode = ""
		case fetcher.OutcomeFailed:
			if errorCode == "" {
				errorCode = "unknown_fetch_failure"
			}
		default:
			return fmt.Errorf("unsupported fetch outcome %q", outcome)
		}
	} else if errorCode == "" {
		errorCode = "retryable_fetch_failure"
	}
	var statusCode *int
	if attempt.StatusCode != 0 {
		statusCode = &attempt.StatusCode
	}
	var finalURL *string
	if attempt.FinalURL != "" {
		finalURL = &attempt.FinalURL
	}
	var retryAfter *time.Time
	if !attempt.RetryAfter.IsZero() {
		retryAfter = &attempt.RetryAfter
	}
	durationMilliseconds := attempt.Duration.Milliseconds()
	if durationMilliseconds < 0 {
		durationMilliseconds = 0
	}
	if durationMilliseconds > math.MaxInt32 {
		durationMilliseconds = math.MaxInt32
	}
	_, err := transaction.Exec(ctx, `
		insert into app.source_fetches (
			endpoint_id, attempted_at, completed_at, outcome, status_code,
			final_url, content_type, compressed_bytes, bytes, duration_ms,
			error_code, retry_after, etag, last_modified, raw_sha256, object_key
		) values (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			nullif($11, ''), $12, nullif($13, ''), nullif($14, ''), $15, $16
		)`,
		endpointID,
		attempt.AttemptedAt,
		attempt.CompletedAt,
		string(outcome),
		statusCode,
		finalURL,
		attempt.ContentType,
		attempt.CompressedBytes,
		attempt.Bytes,
		int32(durationMilliseconds),
		errorCode,
		retryAfter,
		attempt.ETag,
		attempt.LastModified,
		digest,
		objectKey,
	)
	if err != nil {
		return fmt.Errorf("record endpoint %s fetch attempt: %w", endpointID, err)
	}
	return nil
}

func updateEndpoint(ctx context.Context, transaction pgx.Tx, endpointID string, outcome fetcher.Outcome, attempt fetcher.Attempt) error {
	succeeded := outcome == fetcher.OutcomeStored || outcome == fetcher.OutcomeMetadataOnly || outcome == fetcher.OutcomeNotModified
	_, err := transaction.Exec(ctx, `
		update app.source_endpoints
		set last_attempt_at = $2,
			last_success_at = case when $3 then $2 else last_success_at end,
			health_state = case
				when health_state = 'paused' then 'paused'
				when $3 then 'healthy'
				else 'degraded'
			end,
			updated_at = now()
		where id = $1::uuid`, endpointID, attempt.CompletedAt, succeeded)
	if err != nil {
		return fmt.Errorf("update endpoint %s fetch state: %w", endpointID, err)
	}
	return nil
}

func upsertCheckpoint(ctx context.Context, transaction pgx.Tx, endpointID string, result fetcher.Result) error {
	providerState := result.Checkpoint.ProviderState
	if providerState == nil {
		providerState = map[string]any{}
	}
	encodedState, err := json.Marshal(providerState)
	if err != nil {
		return fmt.Errorf("encode endpoint %s provider state: %w", endpointID, err)
	}
	_, err = transaction.Exec(ctx, `
		insert into app.source_checkpoints (
			endpoint_id, cursor, etag, last_modified, provider_state, updated_at
		) values ($1::uuid, nullif($2, ''), nullif($3, ''), nullif($4, ''), $5, now())
		on conflict (endpoint_id) do update set
			cursor = excluded.cursor,
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			provider_state = excluded.provider_state,
			updated_at = now()`,
		endpointID,
		result.Checkpoint.Cursor,
		result.Checkpoint.ETag,
		result.Checkpoint.LastModified,
		encodedState,
	)
	if err != nil {
		return fmt.Errorf("upsert endpoint %s checkpoint: %w", endpointID, err)
	}
	return nil
}

func recordRawDocument(ctx context.Context, transaction pgx.Tx, sourceID string, contentPolicy string, result fetcher.Result, attempt fetcher.Attempt) error {
	var objectKey *string
	if result.Outcome == fetcher.OutcomeStored {
		objectKey = &result.ObjectKey
	}
	_, err := transaction.Exec(ctx, `
		insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256,
			first_seen_at, first_fetched_at, content_policy
		) values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (source_id, canonical_url, raw_sha256) do nothing`,
		sourceID,
		attempt.FinalURL,
		objectKey,
		result.SHA256[:],
		attempt.AttemptedAt,
		attempt.CompletedAt,
		contentPolicy,
	)
	if err != nil {
		return fmt.Errorf("record raw document for source %q: %w", sourceID, err)
	}
	return nil
}
