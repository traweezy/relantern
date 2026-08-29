package pgstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/extraction"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("structured extraction PostgreSQL store requires a database pool")
	}
	return &Store{pool: pool}, nil
}

func (store *Store) Prepare(
	ctx context.Context,
	request extraction.ProcessRequest,
	startedAt time.Time,
) (extraction.PreparedRun, error) {
	if strings.TrimSpace(request.ItemID) == "" || strings.TrimSpace(request.RevisionID) == "" || startedAt.IsZero() {
		return extraction.PreparedRun{}, fmt.Errorf("%w: item, revision, and start time are required", extraction.ErrInvalidTarget)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return extraction.PreparedRun{}, fmt.Errorf("begin structured extraction preparation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	prepared := extraction.PreparedRun{ItemID: request.ItemID, RevisionID: request.RevisionID}
	var currentRevisionID string
	err = transaction.QueryRow(ctx, `
		select
			item.current_revision_id::text,
			item.title,
			item.first_seen_at,
			revision.normalized_text_object_key,
			revision.normalized_sha256,
			revision.normalized_bytes,
			revision.outline,
			raw_document.source_id,
			item_source.source_tier
		from app.items item
		join app.content_revisions revision on revision.id = item.current_revision_id
		join app.raw_documents raw_document on raw_document.id = revision.raw_document_id
		join app.item_sources item_source
			on item_source.item_id = item.id and item_source.revision_id = revision.id
		where item.id = $1::uuid`, request.ItemID).Scan(
		&currentRevisionID,
		&prepared.Title,
		&prepared.FirstSeenAt,
		&prepared.ObjectKey,
		&prepared.NormalizedHash,
		&prepared.NormalizedBytes,
		&prepared.Outline,
		&prepared.SourceID,
		&prepared.SourceTier,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return extraction.PreparedRun{}, fmt.Errorf("%w: item or current source metadata does not exist", extraction.ErrInvalidTarget)
	}
	if err != nil {
		return extraction.PreparedRun{}, fmt.Errorf("select structured extraction target: %w", err)
	}
	if currentRevisionID != request.RevisionID {
		if err := transaction.Commit(ctx); err != nil {
			return extraction.PreparedRun{}, fmt.Errorf("commit obsolete structured extraction lookup: %w", err)
		}
		prepared.Obsolete = true
		prepared.State = "obsolete"
		return prepared, nil
	}

	err = transaction.QueryRow(ctx, `
		select id::text, model_id, reasoning, verbosity, enabled_tools, max_output_tokens
		from app.model_configs
		where role = 'fast' and provider = 'openai' and enabled
		order by valid_from desc, id desc
		limit 1`).Scan(
		&prepared.Model.ID,
		&prepared.Model.ModelID,
		&prepared.Model.Reasoning,
		&prepared.Model.Verbosity,
		&prepared.Model.EnabledTools,
		&prepared.Model.MaxOutputTokens,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return extraction.PreparedRun{}, fmt.Errorf("%w: no enabled fast model config", extraction.ErrConfigurationDrift)
	}
	if err != nil {
		return extraction.PreparedRun{}, fmt.Errorf("select fast model config: %w", err)
	}
	err = transaction.QueryRow(ctx, `
		select id::text, semantic_version, prompt_sha256, schema_version, schema_sha256
		from app.prompt_versions
		where purpose = $1 and active
		order by created_at desc, id desc
		limit 1`, extraction.Purpose).Scan(
		&prepared.Prompt.ID,
		&prepared.Prompt.SemanticVersion,
		&prepared.Prompt.PromptSHA256,
		&prepared.Prompt.SchemaVersion,
		&prepared.Prompt.SchemaSHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return extraction.PreparedRun{}, fmt.Errorf("%w: no active extraction prompt", extraction.ErrConfigurationDrift)
	}
	if err != nil {
		return extraction.PreparedRun{}, fmt.Errorf("select structured extraction prompt: %w", err)
	}

	commandTag, err := transaction.Exec(ctx, `
		insert into app.ai_runs (
			item_id, revision_id, purpose, model_config_id, prompt_version_id,
			background, state, input_sha256, started_at
		) values ($1::uuid, $2::uuid, $3, $4::uuid, $5::uuid, false, 'pending', $6, $7)
		on conflict (
			item_id, revision_id, purpose, model_config_id, prompt_version_id, input_sha256
		)
		do nothing`,
		request.ItemID,
		request.RevisionID,
		extraction.Purpose,
		prepared.Model.ID,
		prepared.Prompt.ID,
		prepared.NormalizedHash,
		startedAt,
	)
	if err != nil {
		return extraction.PreparedRun{}, fmt.Errorf("create structured extraction run: %w", err)
	}
	err = transaction.QueryRow(ctx, `
		select id::text, state
		from app.ai_runs
		where item_id = $1::uuid
			and revision_id = $2::uuid
			and purpose = $3
			and model_config_id = $4::uuid
			and prompt_version_id = $5::uuid
			and input_sha256 = $6
		for update`,
		request.ItemID,
		request.RevisionID,
		extraction.Purpose,
		prepared.Model.ID,
		prepared.Prompt.ID,
		prepared.NormalizedHash,
	).Scan(&prepared.RunID, &prepared.State)
	if err != nil {
		return extraction.PreparedRun{}, fmt.Errorf("select structured extraction run: %w", err)
	}
	if commandTag.RowsAffected() == 1 {
		if _, err := transaction.Exec(ctx, `
			update app.items
			set lifecycle_state = 'extracting', updated_at = $3
			where id = $1::uuid and current_revision_id = $2::uuid`,
			request.ItemID,
			request.RevisionID,
			startedAt,
		); err != nil {
			return extraction.PreparedRun{}, fmt.Errorf("mark item extracting: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return extraction.PreparedRun{}, fmt.Errorf("commit structured extraction preparation: %w", err)
	}
	return prepared, nil
}

func (store *Store) ReserveAttempt(
	ctx context.Context,
	prepared extraction.PreparedRun,
	estimatedInputTokens int64,
	monthlySoftUSD extraction.USD,
	monthlyHardUSD extraction.USD,
	startedAt time.Time,
) (extraction.AttemptReservation, error) {
	if estimatedInputTokens < 1 || startedAt.IsZero() {
		return extraction.AttemptReservation{}, errors.New("valid token estimate, budgets, and attempt time are required")
	}
	if err := extraction.ValidateBudgetRange(monthlySoftUSD, monthlyHardUSD); err != nil {
		return extraction.AttemptReservation{}, fmt.Errorf("validate OpenAI budget range: %w", err)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return extraction.AttemptReservation{}, fmt.Errorf("begin OpenAI attempt reservation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var runState string
	err = transaction.QueryRow(ctx, `
		select run.state
		from app.ai_runs run
		where run.id = $1::uuid
		for update of run`, prepared.RunID).Scan(&runState)
	if err != nil {
		return extraction.AttemptReservation{}, fmt.Errorf("lock structured extraction run: %w", err)
	}
	if runState == "completed" || runState == "needs_review" || runState == "obsolete" {
		return extraction.AttemptReservation{}, errors.New("completed structured extraction runs cannot reserve another attempt")
	}
	if _, err := transaction.Exec(ctx, `
		update app.ai_run_attempts
		set
			state = 'failed',
			estimated_cost_usd = reserved_cost_usd,
			completed_at = $2,
			error_code = 'worker_interrupted'
		where ai_run_id = $1::uuid and state = 'reserved'`, prepared.RunID, startedAt); err != nil {
		return extraction.AttemptReservation{}, fmt.Errorf("close interrupted OpenAI attempt: %w", err)
	}
	monthKey := startedAt.Format("2006-01")
	if _, err := transaction.Exec(ctx, `
		select pg_advisory_xact_lock(hashtextextended('openai-budget:' || $1, 0))`, monthKey); err != nil {
		return extraction.AttemptReservation{}, fmt.Errorf("lock monthly OpenAI budget: %w", err)
	}
	var reservedCost string
	var softExceeded bool
	var hardExceeded bool
	err = transaction.QueryRow(ctx, `
		with model_cost as (
			select round((
				($2::bigint * config.input_usd_per_million)
				+ (config.max_output_tokens::bigint * config.output_usd_per_million)
			) / 1000000, 8)::numeric(14,8) as reserved_cost_usd
			from app.ai_runs run
			join app.model_configs config on config.id = run.model_config_id
			where run.id = $1::uuid
		), monthly_cost as (
			select coalesce(sum(estimated_cost_usd), 0)::numeric(14,8) as cost_usd
			from app.ai_run_attempts
			where started_at >= date_trunc('month', $5::timestamptz)
				and started_at < date_trunc('month', $5::timestamptz) + interval '1 month'
		)
		select
			model_cost.reserved_cost_usd::text,
			monthly_cost.cost_usd + model_cost.reserved_cost_usd > $3::numeric,
			monthly_cost.cost_usd + model_cost.reserved_cost_usd > $4::numeric
		from model_cost cross join monthly_cost`,
		prepared.RunID,
		estimatedInputTokens,
		string(monthlySoftUSD),
		string(monthlyHardUSD),
		startedAt,
	).Scan(&reservedCost, &softExceeded, &hardExceeded)
	if err != nil {
		return extraction.AttemptReservation{}, fmt.Errorf("calculate monthly OpenAI budget: %w", err)
	}
	if hardExceeded {
		if _, err := transaction.Exec(ctx, `
			update app.ai_runs
			set state = 'budget_blocked', error_code = 'budget_exceeded', updated_at = $2
			where id = $1::uuid`, prepared.RunID, startedAt); err != nil {
			return extraction.AttemptReservation{}, fmt.Errorf("mark structured extraction budget blocked: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return extraction.AttemptReservation{}, fmt.Errorf("commit OpenAI budget block: %w", err)
		}
		return extraction.AttemptReservation{}, extraction.ErrBudgetExceeded
	}
	var reservation extraction.AttemptReservation
	err = transaction.QueryRow(ctx, `
		insert into app.ai_run_attempts (
			ai_run_id, attempt_number, state, reserved_cost_usd,
			estimated_cost_usd, budget_soft_alert, started_at
		) values (
			$1::uuid,
			(select coalesce(max(attempt_number), 0) + 1 from app.ai_run_attempts where ai_run_id = $1::uuid),
			'reserved', $2, $2, $3, $4
		) returning id, attempt_number`, prepared.RunID, reservedCost, softExceeded, startedAt).Scan(
		&reservation.ID,
		&reservation.Number,
	)
	if err != nil {
		return extraction.AttemptReservation{}, fmt.Errorf("reserve OpenAI attempt: %w", err)
	}
	reservation.BudgetSoftExceeded = softExceeded
	if _, err := transaction.Exec(ctx, `
		update app.ai_runs
		set state = 'running', attempt_count = $2, error_code = null, updated_at = $3
		where id = $1::uuid`, prepared.RunID, reservation.Number, startedAt); err != nil {
		return extraction.AttemptReservation{}, fmt.Errorf("mark structured extraction running: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return extraction.AttemptReservation{}, fmt.Errorf("commit OpenAI attempt reservation: %w", err)
	}
	return reservation, nil
}

func (store *Store) RecordAttempt(
	ctx context.Context,
	prepared extraction.PreparedRun,
	reservation extraction.AttemptReservation,
	response extraction.ProviderResponse,
	errorCode string,
	completedAt time.Time,
) error {
	if completedAt.IsZero() || response.Usage.InputTokens < 0 || response.Usage.CachedInputTokens < 0 ||
		response.Usage.CachedInputTokens > response.Usage.InputTokens || response.Usage.OutputTokens < 0 {
		return errors.New("valid OpenAI completion time and usage counters are required")
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin OpenAI attempt completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	state := "completed"
	if errorCode != "" {
		state = "failed"
	}
	commandTag, err := transaction.Exec(ctx, `
		update app.ai_run_attempts attempt
		set
			state = $4::text,
			provider_response_id = nullif($5::text, ''),
			input_tokens = $6::bigint,
			cached_input_tokens = $7::bigint,
			output_tokens = $8::bigint,
			estimated_cost_usd = case
				when $9::text <> '' and $6::bigint = 0 and $8::bigint = 0 then attempt.reserved_cost_usd
				else round((
					(($6::bigint - $7::bigint) * config.input_usd_per_million)
					+ ($7::bigint * config.cached_input_usd_per_million)
					+ ($8::bigint * config.output_usd_per_million)
				) / 1000000, 8)
			end,
			completed_at = $10::timestamptz,
			error_code = nullif($9::text, '')
		from app.ai_runs run
		join app.model_configs config on config.id = run.model_config_id
		where attempt.id = $1
			and attempt.ai_run_id = $2::uuid
			and attempt.attempt_number = $3
			and attempt.ai_run_id = run.id
			and attempt.state = 'reserved'`,
		reservation.ID,
		prepared.RunID,
		reservation.Number,
		state,
		response.ID,
		response.Usage.InputTokens,
		response.Usage.CachedInputTokens,
		response.Usage.OutputTokens,
		errorCode,
		completedAt,
	)
	if err != nil {
		return fmt.Errorf("record OpenAI attempt completion: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("OpenAI attempt reservation is missing or already completed")
	}
	if err := refreshRunTotals(ctx, transaction, prepared.RunID, response.ID, completedAt); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit OpenAI attempt completion: %w", err)
	}
	return nil
}

func (store *Store) Complete(
	ctx context.Context,
	prepared extraction.PreparedRun,
	completion extraction.Completion,
	completedAt time.Time,
) (extraction.ProcessResult, error) {
	if completion.CompletedState != "completed" && completion.CompletedState != "needs_review" {
		return extraction.ProcessResult{}, errors.New("structured extraction completion state must be completed or needs_review")
	}
	if completedAt.IsZero() {
		return extraction.ProcessResult{}, errors.New("structured extraction completion time is required")
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return extraction.ProcessResult{}, fmt.Errorf("begin structured extraction completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var state string
	err = transaction.QueryRow(ctx, `select state from app.ai_runs where id = $1::uuid for update`, prepared.RunID).Scan(&state)
	if err != nil {
		return extraction.ProcessResult{}, fmt.Errorf("lock structured extraction completion: %w", err)
	}
	if state == "completed" || state == "needs_review" {
		var claimCount int
		if err := transaction.QueryRow(ctx, `select count(*) from app.claims where ai_run_id = $1::uuid`, prepared.RunID).Scan(&claimCount); err != nil {
			return extraction.ProcessResult{}, fmt.Errorf("count completed extraction claims: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return extraction.ProcessResult{}, fmt.Errorf("commit completed extraction lookup: %w", err)
		}
		return extraction.ProcessResult{RunID: prepared.RunID, ClaimCount: claimCount, AlreadyCompleted: true, NeedsReview: state == "needs_review"}, nil
	}
	var currentRevisionID string
	if err := transaction.QueryRow(ctx, `select current_revision_id::text from app.items where id = $1::uuid for update`, prepared.ItemID).Scan(&currentRevisionID); err != nil {
		return extraction.ProcessResult{}, fmt.Errorf("lock extraction item: %w", err)
	}
	if currentRevisionID != prepared.RevisionID {
		if _, err := transaction.Exec(ctx, `
			update app.ai_runs
			set state = 'obsolete', completed_at = $2, error_code = 'obsolete_revision', updated_at = $2
			where id = $1::uuid`, prepared.RunID, completedAt); err != nil {
			return extraction.ProcessResult{}, fmt.Errorf("mark extraction run obsolete: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return extraction.ProcessResult{}, fmt.Errorf("commit obsolete extraction run: %w", err)
		}
		return extraction.ProcessResult{RunID: prepared.RunID, Obsolete: true}, nil
	}
	validatedOutput, err := json.Marshal(completion.Output)
	if err != nil {
		return extraction.ProcessResult{}, fmt.Errorf("encode validated structured extraction: %w", err)
	}
	spanIndex := make(map[string]extraction.EvidenceSpan, len(completion.Spans))
	for _, span := range completion.Spans {
		spanIndex[span.ID] = span
	}
	for claimIndex, claim := range completion.Output.Claims {
		normalizedValue, marshalError := json.Marshal(claim.NormalizedValue)
		if marshalError != nil {
			return extraction.ProcessResult{}, fmt.Errorf("encode normalized claim value: %w", marshalError)
		}
		verificationState := "verified_span"
		if completion.NeedsReview && claim.Material {
			verificationState = "review_required"
		}
		var claimID string
		err := transaction.QueryRow(ctx, `
			insert into app.claims (
				item_id, revision_id, ai_run_id, claim_index, claim_type,
				claim_text, normalized_value, confidence, material, verification_state
			) values ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::jsonb, $8, $9, $10)
			returning id::text`,
			prepared.ItemID,
			prepared.RevisionID,
			prepared.RunID,
			claimIndex,
			claim.ClaimType,
			claim.ClaimText,
			normalizedValue,
			claim.Confidence,
			claim.Material,
			verificationState,
		).Scan(&claimID)
		if err != nil {
			return extraction.ProcessResult{}, fmt.Errorf("persist atomic extraction claim: %w", err)
		}
		for _, spanID := range claim.EvidenceSpanIDs {
			span, exists := spanIndex[spanID]
			if !exists {
				return extraction.ProcessResult{}, fmt.Errorf("claim references unvalidated span %q", spanID)
			}
			quoteHash := sha256.Sum256([]byte(span.Text))
			if _, err := transaction.Exec(ctx, `
				insert into app.evidence_spans (
					claim_id, revision_id, span_identifier, section_path,
					start_offset, end_offset, quote_hash
				) values ($1::uuid, $2::uuid, $3, $4, $5, $6, $7)`,
				claimID,
				prepared.RevisionID,
				span.ID,
				span.SectionPath,
				span.StartOffset,
				span.EndOffset,
				quoteHash[:],
			); err != nil {
				return extraction.ProcessResult{}, fmt.Errorf("persist claim evidence span: %w", err)
			}
		}
	}
	if _, err := transaction.Exec(ctx, `
		update app.ai_runs
		set
			provider_response_id = $2,
			state = $3,
			validated_output = $4::jsonb,
			completed_at = $5,
			error_code = null,
			updated_at = $5
		where id = $1::uuid`, prepared.RunID, completion.ProviderID, completion.CompletedState, string(validatedOutput), completedAt); err != nil {
		return extraction.ProcessResult{}, fmt.Errorf("complete structured extraction run: %w", err)
	}
	if err := refreshRunTotals(ctx, transaction, prepared.RunID, completion.ProviderID, completedAt); err != nil {
		return extraction.ProcessResult{}, err
	}
	itemLifecycle := "ready"
	if completion.NeedsReview {
		itemLifecycle = "needs_review"
	}
	if _, err := transaction.Exec(ctx, `
		update app.items
		set event_type = $3, lifecycle_state = $4, updated_at = $5
		where id = $1::uuid and current_revision_id = $2::uuid`,
		prepared.ItemID,
		prepared.RevisionID,
		completion.Output.EventType,
		itemLifecycle,
		completedAt,
	); err != nil {
		return extraction.ProcessResult{}, fmt.Errorf("apply structured extraction state: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return extraction.ProcessResult{}, fmt.Errorf("commit structured extraction completion: %w", err)
	}
	return extraction.ProcessResult{
		RunID:       prepared.RunID,
		ClaimCount:  len(completion.Output.Claims),
		NeedsReview: completion.NeedsReview,
	}, nil
}

func (store *Store) Fail(
	ctx context.Context,
	prepared extraction.PreparedRun,
	errorCode string,
	failedAt time.Time,
) error {
	if strings.TrimSpace(errorCode) == "" || failedAt.IsZero() {
		return errors.New("structured extraction failure requires an error code and timestamp")
	}
	runState := "needs_review"
	itemState := "needs_review"
	var completedAt *time.Time
	if errorCode == "provider_error" {
		runState = "failed_retryable"
		itemState = "failed_retryable"
	} else {
		completedAt = &failedAt
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin structured extraction failure: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `
		update app.ai_runs
		set state = $2, completed_at = $3, error_code = $4, updated_at = $5
		where id = $1::uuid`, prepared.RunID, runState, completedAt, errorCode, failedAt); err != nil {
		return fmt.Errorf("record structured extraction failure: %w", err)
	}
	if err := refreshRunTotals(ctx, transaction, prepared.RunID, "", failedAt); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `
		update app.items
		set lifecycle_state = $3, updated_at = $4
		where id = $1::uuid and current_revision_id = $2::uuid`,
		prepared.ItemID,
		prepared.RevisionID,
		itemState,
		failedAt,
	); err != nil {
		return fmt.Errorf("record item extraction failure: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit structured extraction failure: %w", err)
	}
	return nil
}

func refreshRunTotals(
	ctx context.Context,
	transaction pgx.Tx,
	runID string,
	providerResponseID string,
	updatedAt time.Time,
) error {
	_, err := transaction.Exec(ctx, `
		update app.ai_runs run
		set
			provider_response_id = coalesce(nullif($2, ''), run.provider_response_id),
			input_tokens = totals.input_tokens,
			cached_input_tokens = totals.cached_input_tokens,
			output_tokens = totals.output_tokens,
			estimated_cost_usd = totals.estimated_cost_usd,
			updated_at = $3
		from (
			select
				coalesce(sum(input_tokens), 0)::bigint as input_tokens,
				coalesce(sum(cached_input_tokens), 0)::bigint as cached_input_tokens,
				coalesce(sum(output_tokens), 0)::bigint as output_tokens,
				coalesce(sum(estimated_cost_usd), 0)::numeric(14, 8) as estimated_cost_usd
			from app.ai_run_attempts
			where ai_run_id = $1::uuid
		) totals
		where run.id = $1::uuid`, runID, providerResponseID, updatedAt)
	if err != nil {
		return fmt.Errorf("refresh structured extraction usage totals: %w", err)
	}
	return nil
}
