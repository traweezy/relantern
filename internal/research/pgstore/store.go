package pgstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/research"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("research PostgreSQL store requires a database pool")
	}
	return &Store{pool: pool}, nil
}

func (store *Store) Prepare(
	ctx context.Context,
	request research.ProcessRequest,
	startedAt time.Time,
) (research.PreparedRun, error) {
	if strings.TrimSpace(request.ClusterID) == "" || startedAt.IsZero() {
		return research.PreparedRun{}, fmt.Errorf("%w: cluster and start time are required", research.ErrInvalidTarget)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return research.PreparedRun{}, fmt.Errorf("begin research preparation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	prepared := research.PreparedRun{ClusterID: request.ClusterID}
	err = transaction.QueryRow(ctx, `
		select cluster.primary_item_id::text, item.current_revision_id::text, cluster.title
		from app.story_clusters cluster
		join app.items item on item.id = cluster.primary_item_id
		where cluster.id = $1::uuid
		for update of cluster`, request.ClusterID).Scan(&prepared.ItemID, &prepared.RevisionID, &prepared.Title)
	if errors.Is(err, pgx.ErrNoRows) {
		return research.PreparedRun{}, fmt.Errorf("%w: story cluster does not exist", research.ErrInvalidTarget)
	}
	if err != nil {
		return research.PreparedRun{}, fmt.Errorf("select research cluster: %w", err)
	}
	prepared.Claims, err = selectClaims(ctx, transaction, request.ClusterID)
	if err != nil {
		return research.PreparedRun{}, err
	}
	_, prepared.InputHash, err = research.EncodeValidatedFacts(prepared.Title, prepared.ClusterID, prepared.Claims)
	if err != nil {
		return research.PreparedRun{}, fmt.Errorf("prepare validated research facts: %w", err)
	}
	if err := selectEnabledRegistry(ctx, transaction, &prepared); err != nil {
		return research.PreparedRun{}, err
	}
	commandTag, err := transaction.Exec(ctx, `
		insert into app.ai_runs (
			item_id, revision_id, cluster_id, purpose, model_config_id,
			prompt_version_id, background, state, input_sha256, started_at
		) values ($1::uuid, $2::uuid, $3::uuid, $4, $5::uuid, $6::uuid, true, 'pending', $7, $8)
		on conflict (
			item_id, revision_id, purpose, model_config_id, prompt_version_id, input_sha256
		)
		do nothing`,
		prepared.ItemID,
		prepared.RevisionID,
		prepared.ClusterID,
		research.Purpose,
		prepared.Model.ID,
		prepared.Prompt.ID,
		prepared.InputHash,
		startedAt,
	)
	if err != nil {
		return research.PreparedRun{}, fmt.Errorf("create research run: %w", err)
	}
	err = transaction.QueryRow(ctx, `
		select id::text, state, coalesce(provider_response_id, ''), input_sha256
		from app.ai_runs
		where item_id = $1::uuid and revision_id = $2::uuid and purpose = $3
			and model_config_id = $4::uuid and prompt_version_id = $5::uuid
			and input_sha256 = $6
		for update`,
		prepared.ItemID,
		prepared.RevisionID,
		research.Purpose,
		prepared.Model.ID,
		prepared.Prompt.ID,
		prepared.InputHash,
	).Scan(&prepared.RunID, &prepared.State, &prepared.ProviderResponseID, &prepared.InputHash)
	if err != nil {
		return research.PreparedRun{}, fmt.Errorf("select research run: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		_, currentHash, hashError := research.EncodeValidatedFacts(prepared.Title, prepared.ClusterID, prepared.Claims)
		if hashError != nil {
			return research.PreparedRun{}, hashError
		}
		if !bytes.Equal(currentHash, prepared.InputHash) && prepared.State != "completed" && prepared.State != "needs_review" {
			if _, err := transaction.Exec(ctx, `
				update app.ai_runs
				set state = 'obsolete', error_code = 'cluster_changed', completed_at = $2, updated_at = $2
				where id = $1::uuid`, prepared.RunID, startedAt); err != nil {
				return research.PreparedRun{}, fmt.Errorf("mark changed research run obsolete: %w", err)
			}
			prepared.Obsolete = true
			prepared.State = "obsolete"
		}
	}
	if err := transaction.QueryRow(ctx, `
		select count(*)
		from app.ai_run_attempts
		where ai_run_id = $1::uuid and error_code = 'schema_invalid'`, prepared.RunID).Scan(
		&prepared.SchemaFailures,
	); err != nil {
		return research.PreparedRun{}, fmt.Errorf("count research schema failures: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return research.PreparedRun{}, fmt.Errorf("commit research preparation: %w", err)
	}
	return prepared, nil
}

func (store *Store) LoadPending(
	ctx context.Context,
	request research.PollRequest,
) (research.PreparedRun, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return research.PreparedRun{}, fmt.Errorf("begin pending research lookup: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	prepared := research.PreparedRun{}
	err = transaction.QueryRow(ctx, `
		select
			run.id::text, run.cluster_id::text, run.item_id::text, run.revision_id::text,
			cluster.title, run.input_sha256, run.state, coalesce(run.error_code, ''),
			coalesce(run.provider_response_id, ''),
			config.id::text, config.model_id, config.reasoning, config.verbosity,
			config.enabled_tools, config.max_output_tokens,
			prompt.id::text, prompt.semantic_version, prompt.prompt_sha256,
			prompt.schema_version, prompt.schema_sha256
		from app.ai_runs run
		join app.story_clusters cluster on cluster.id = run.cluster_id
		join app.model_configs config on config.id = run.model_config_id
		join app.prompt_versions prompt on prompt.id = run.prompt_version_id
		where run.purpose = $1
			and run.provider_response_id = $2
			and (
				nullif($3::text, '') is null
				or run.id = nullif($3::text, '')::uuid
			)
		for update of run`, research.Purpose, request.ResponseID, request.RunID).Scan(
		&prepared.RunID,
		&prepared.ClusterID,
		&prepared.ItemID,
		&prepared.RevisionID,
		&prepared.Title,
		&prepared.InputHash,
		&prepared.State,
		&prepared.ErrorCode,
		&prepared.ProviderResponseID,
		&prepared.Model.ID,
		&prepared.Model.ModelID,
		&prepared.Model.Reasoning,
		&prepared.Model.Verbosity,
		&prepared.Model.EnabledTools,
		&prepared.Model.MaxOutputTokens,
		&prepared.Prompt.ID,
		&prepared.Prompt.SemanticVersion,
		&prepared.Prompt.PromptSHA256,
		&prepared.Prompt.SchemaVersion,
		&prepared.Prompt.SchemaSHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return research.PreparedRun{}, fmt.Errorf("%w: background research run does not exist", research.ErrInvalidTarget)
	}
	if err != nil {
		return research.PreparedRun{}, fmt.Errorf("select pending research run: %w", err)
	}
	prepared.Claims, err = selectClaims(ctx, transaction, prepared.ClusterID)
	if err != nil {
		return research.PreparedRun{}, err
	}
	var currentItemID string
	var currentRevisionID string
	if err := transaction.QueryRow(ctx, `
		select cluster.primary_item_id::text, item.current_revision_id::text
		from app.story_clusters cluster
		join app.items item on item.id = cluster.primary_item_id
		where cluster.id = $1::uuid`, prepared.ClusterID).Scan(&currentItemID, &currentRevisionID); err != nil {
		return research.PreparedRun{}, fmt.Errorf("select current research target: %w", err)
	}
	_, currentHash, err := research.EncodeValidatedFacts(prepared.Title, prepared.ClusterID, prepared.Claims)
	if err != nil {
		return research.PreparedRun{}, err
	}
	if currentItemID != prepared.ItemID || currentRevisionID != prepared.RevisionID || !bytes.Equal(currentHash, prepared.InputHash) {
		if _, err := transaction.Exec(ctx, `
			update app.ai_runs
			set state = 'obsolete', error_code = 'cluster_changed', completed_at = now(), updated_at = now()
			where id = $1::uuid and state not in ('completed', 'needs_review', 'obsolete')`, prepared.RunID); err != nil {
			return research.PreparedRun{}, fmt.Errorf("mark pending research obsolete: %w", err)
		}
		prepared.Obsolete = true
		prepared.State = "obsolete"
	}
	if err := transaction.QueryRow(ctx, `
		select count(*)
		from app.ai_run_attempts
		where ai_run_id = $1::uuid and error_code = 'schema_invalid'`, prepared.RunID).Scan(
		&prepared.SchemaFailures,
	); err != nil {
		return research.PreparedRun{}, fmt.Errorf("count pending research schema failures: %w", err)
	}
	if !prepared.Obsolete && prepared.State == "running" {
		err = transaction.QueryRow(ctx, `
			select id, attempt_number, reserved_tool_calls, budget_soft_alert
			from app.ai_run_attempts
			where ai_run_id = $1::uuid and state = 'reserved'
			order by attempt_number desc
			limit 1`, prepared.RunID).Scan(
			&prepared.Reservation.ID,
			&prepared.Reservation.Number,
			&prepared.Reservation.MaximumToolCalls,
			&prepared.Reservation.BudgetSoftExceeded,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return research.PreparedRun{}, errors.New("pending research run has no active cost reservation")
		}
		if err != nil {
			return research.PreparedRun{}, fmt.Errorf("select pending research reservation: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return research.PreparedRun{}, fmt.Errorf("commit pending research lookup: %w", err)
	}
	return prepared, nil
}

func (store *Store) ReserveAttempt(
	ctx context.Context,
	prepared research.PreparedRun,
	estimatedInputTokens int64,
	config research.Config,
	startedAt time.Time,
) (research.AttemptReservation, error) {
	if estimatedInputTokens < 1 || startedAt.IsZero() || config.MaximumToolCalls < 1 || config.DailyWebSearchLimit < config.MaximumToolCalls {
		return research.AttemptReservation{}, errors.New("valid research estimate, limits, and attempt time are required")
	}
	if err := extraction.ValidateBudgetRange(config.MonthlySoftUSD, config.MonthlyHardUSD); err != nil {
		return research.AttemptReservation{}, err
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return research.AttemptReservation{}, fmt.Errorf("begin research reservation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var runState string
	var attemptCount int
	if err := transaction.QueryRow(ctx, `
		select state, attempt_count from app.ai_runs where id = $1::uuid for update`, prepared.RunID).Scan(
		&runState, &attemptCount,
	); err != nil {
		return research.AttemptReservation{}, fmt.Errorf("lock research run: %w", err)
	}
	if runState == "completed" || runState == "needs_review" || runState == "obsolete" || runState == "budget_blocked" {
		return research.AttemptReservation{}, errors.New("terminal research run cannot reserve another attempt")
	}
	if _, err := transaction.Exec(ctx, `
		update app.ai_run_attempts
		set state = 'failed', tool_calls = reserved_tool_calls,
			estimated_cost_usd = reserved_cost_usd, completed_at = $2,
			error_code = 'worker_interrupted'
		where ai_run_id = $1::uuid and state = 'reserved'`, prepared.RunID, startedAt); err != nil {
		return research.AttemptReservation{}, fmt.Errorf("close interrupted research attempt: %w", err)
	}
	if attemptCount >= research.MaximumProviderAttempts {
		if _, err := transaction.Exec(ctx, `
			update app.ai_runs
			set state = 'needs_review', error_code = 'attempts_exhausted',
				completed_at = $2, updated_at = $2
			where id = $1::uuid`, prepared.RunID, startedAt); err != nil {
			return research.AttemptReservation{}, fmt.Errorf("mark research attempts exhausted: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return research.AttemptReservation{}, fmt.Errorf("commit research attempts exhausted: %w", err)
		}
		return research.AttemptReservation{}, research.ErrAttemptsExhausted
	}
	monthKey := startedAt.Format("2006-01")
	dayKey := startedAt.Format("2006-01-02")
	if _, err := transaction.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('openai-budget:' || $1, 0))`, monthKey); err != nil {
		return research.AttemptReservation{}, fmt.Errorf("lock monthly OpenAI budget: %w", err)
	}
	if _, err := transaction.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('openai-web-search:' || $1, 0))`, dayKey); err != nil {
		return research.AttemptReservation{}, fmt.Errorf("lock daily web-search budget: %w", err)
	}
	var reservedCost string
	var softExceeded bool
	var hardExceeded bool
	var searchLimitExceeded bool
	err = transaction.QueryRow(ctx, `
		with model_cost as (
			select round((
				($2::bigint * config.input_usd_per_million)
				+ (config.max_output_tokens::bigint * config.output_usd_per_million)
			) / 1000000 + ($3::integer * config.web_search_usd_per_call), 8)::numeric(14,8)
				as reserved_cost_usd
			from app.ai_runs run
			join app.model_configs config on config.id = run.model_config_id
			where run.id = $1::uuid
		), monthly_cost as (
			select coalesce(sum(estimated_cost_usd), 0)::numeric(14,8) as cost_usd
			from app.ai_run_attempts
			where started_at >= date_trunc('month', $6::timestamptz)
				and started_at < date_trunc('month', $6::timestamptz) + interval '1 month'
		), daily_search as (
			select coalesce(sum(case when state = 'reserved' then reserved_tool_calls else tool_calls end), 0)::bigint as calls
			from app.ai_run_attempts
			where started_at >= date_trunc('day', $6::timestamptz)
				and started_at < date_trunc('day', $6::timestamptz) + interval '1 day'
		)
		select
			model_cost.reserved_cost_usd::text,
			monthly_cost.cost_usd + model_cost.reserved_cost_usd > $4::numeric,
			monthly_cost.cost_usd + model_cost.reserved_cost_usd > $5::numeric,
			daily_search.calls + $3::integer > $7::integer
		from model_cost cross join monthly_cost cross join daily_search`,
		prepared.RunID,
		estimatedInputTokens,
		config.MaximumToolCalls,
		string(config.MonthlySoftUSD),
		string(config.MonthlyHardUSD),
		startedAt,
		config.DailyWebSearchLimit,
	).Scan(&reservedCost, &softExceeded, &hardExceeded, &searchLimitExceeded)
	if err != nil {
		return research.AttemptReservation{}, fmt.Errorf("calculate research budgets: %w", err)
	}
	if hardExceeded || searchLimitExceeded {
		errorCode := "budget_exceeded"
		cause := error(extraction.ErrBudgetExceeded)
		if searchLimitExceeded {
			errorCode = "web_search_limit"
			cause = research.ErrWebSearchLimit
		}
		if _, err := transaction.Exec(ctx, `
			update app.ai_runs
			set state = 'budget_blocked', error_code = $2, updated_at = $3
			where id = $1::uuid`, prepared.RunID, errorCode, startedAt); err != nil {
			return research.AttemptReservation{}, fmt.Errorf("mark research budget blocked: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return research.AttemptReservation{}, fmt.Errorf("commit research budget block: %w", err)
		}
		return research.AttemptReservation{}, cause
	}
	reservation := research.AttemptReservation{MaximumToolCalls: config.MaximumToolCalls, BudgetSoftExceeded: softExceeded}
	err = transaction.QueryRow(ctx, `
		insert into app.ai_run_attempts (
			ai_run_id, attempt_number, state, reserved_tool_calls,
			reserved_cost_usd, estimated_cost_usd, budget_soft_alert, started_at
		) values (
			$1::uuid,
			(select coalesce(max(attempt_number), 0) + 1 from app.ai_run_attempts where ai_run_id = $1::uuid),
			'reserved', $2, $3, $3, $4, $5
		) returning id, attempt_number`,
		prepared.RunID,
		config.MaximumToolCalls,
		reservedCost,
		softExceeded,
		startedAt,
	).Scan(&reservation.ID, &reservation.Number)
	if err != nil {
		return research.AttemptReservation{}, fmt.Errorf("reserve research attempt: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.ai_runs
		set state = 'running', attempt_count = $2,
			error_code = null, updated_at = $3
		where id = $1::uuid`, prepared.RunID, reservation.Number, startedAt); err != nil {
		return research.AttemptReservation{}, fmt.Errorf("mark research running: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return research.AttemptReservation{}, fmt.Errorf("commit research reservation: %w", err)
	}
	return reservation, nil
}

func (store *Store) AttachProvider(
	ctx context.Context,
	prepared research.PreparedRun,
	reservation research.AttemptReservation,
	response research.ProviderResponse,
	updatedAt time.Time,
) error {
	if response.ID == "" || len(response.ID) > 255 || updatedAt.IsZero() {
		return errors.New("research provider response ID and update time are required")
	}
	commandTag, err := store.pool.Exec(ctx, `
		with attempt_update as (
			update app.ai_run_attempts
			set provider_response_id = $4
			where id = $1 and ai_run_id = $2::uuid and attempt_number = $3 and state = 'reserved'
			returning ai_run_id
		)
		update app.ai_runs run
		set provider_response_id = $4, state = 'running', updated_at = $5
		from attempt_update
		where run.id = attempt_update.ai_run_id`,
		reservation.ID,
		prepared.RunID,
		reservation.Number,
		response.ID,
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("attach research provider response: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("research reservation is missing or already completed")
	}
	return nil
}

func (store *Store) RecordAttempt(
	ctx context.Context,
	prepared research.PreparedRun,
	reservation research.AttemptReservation,
	response research.ProviderResponse,
	errorCode string,
	completedAt time.Time,
) error {
	if completedAt.IsZero() || response.Usage.InputTokens < 0 || response.Usage.CachedInputTokens < 0 ||
		response.Usage.CachedInputTokens > response.Usage.InputTokens || response.Usage.OutputTokens < 0 ||
		response.Usage.ToolCalls < 0 || response.Usage.ToolCalls > reservation.MaximumToolCalls {
		return errors.New("valid research completion, usage, and tool counters are required")
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin research attempt completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	state := "completed"
	if errorCode != "" {
		state = "failed"
	}
	commandTag, err := transaction.Exec(ctx, `
		update app.ai_run_attempts attempt
		set state = $4, provider_response_id = nullif($5, ''),
			input_tokens = $6, cached_input_tokens = $7, output_tokens = $8,
			tool_calls = $9,
			estimated_cost_usd = case
				when $10::text <> '' and $6::bigint = 0 and $8::bigint = 0 and $9::integer = 0
					then attempt.reserved_cost_usd
				else round((
					(($6::bigint - $7::bigint) * config.input_usd_per_million)
					+ ($7::bigint * config.cached_input_usd_per_million)
					+ ($8::bigint * config.output_usd_per_million)
				) / 1000000 + ($9::integer * config.web_search_usd_per_call), 8)
			end,
			completed_at = $11, error_code = nullif($10, '')
		from app.ai_runs run
		join app.model_configs config on config.id = run.model_config_id
		where attempt.id = $1 and attempt.ai_run_id = $2::uuid
			and attempt.attempt_number = $3 and attempt.ai_run_id = run.id
			and attempt.state = 'reserved'`,
		reservation.ID,
		prepared.RunID,
		reservation.Number,
		state,
		response.ID,
		response.Usage.InputTokens,
		response.Usage.CachedInputTokens,
		response.Usage.OutputTokens,
		response.Usage.ToolCalls,
		errorCode,
		completedAt,
	)
	if err != nil {
		return fmt.Errorf("record research attempt: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("research attempt reservation is missing or already completed")
	}
	if err := refreshRunTotals(ctx, transaction, prepared.RunID, response.ID, completedAt); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit research attempt: %w", err)
	}
	return nil
}

func (store *Store) Complete(
	ctx context.Context,
	prepared research.PreparedRun,
	completion research.Completion,
	completedAt time.Time,
) (research.ProcessResult, error) {
	if completedAt.IsZero() {
		return research.ProcessResult{}, errors.New("research completion time is required")
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return research.ProcessResult{}, fmt.Errorf("begin research completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var state string
	if err := transaction.QueryRow(ctx, `select state from app.ai_runs where id = $1::uuid for update`, prepared.RunID).Scan(&state); err != nil {
		return research.ProcessResult{}, fmt.Errorf("lock research completion: %w", err)
	}
	if state == "completed" || state == "needs_review" {
		var count int
		if err := transaction.QueryRow(ctx, `
			select count(*) from app.research_assertions assertion
			join app.research_briefs brief on brief.id = assertion.research_brief_id
			where brief.ai_run_id = $1::uuid`, prepared.RunID).Scan(&count); err != nil {
			return research.ProcessResult{}, err
		}
		if err := transaction.Commit(ctx); err != nil {
			return research.ProcessResult{}, err
		}
		return research.ProcessResult{RunID: prepared.RunID, ProviderID: completion.ProviderID, AssertionCount: count, AlreadyCompleted: true, NeedsReview: state == "needs_review"}, nil
	}
	var currentItemID string
	var currentRevisionID string
	if err := transaction.QueryRow(ctx, `
		select cluster.primary_item_id::text, item.current_revision_id::text
		from app.story_clusters cluster
		join app.items item on item.id = cluster.primary_item_id
		where cluster.id = $1::uuid
		for update of cluster`, prepared.ClusterID).Scan(&currentItemID, &currentRevisionID); err != nil {
		return research.ProcessResult{}, fmt.Errorf("lock current research target: %w", err)
	}
	currentClaims, err := selectClaims(ctx, transaction, prepared.ClusterID)
	if err != nil {
		return research.ProcessResult{}, err
	}
	_, currentHash, err := research.EncodeValidatedFacts(prepared.Title, prepared.ClusterID, currentClaims)
	if err != nil {
		return research.ProcessResult{}, err
	}
	if currentItemID != prepared.ItemID || currentRevisionID != prepared.RevisionID || !bytes.Equal(currentHash, prepared.InputHash) {
		if _, err := transaction.Exec(ctx, `
			update app.ai_runs
			set state = 'obsolete', error_code = 'cluster_changed', completed_at = $2, updated_at = $2
			where id = $1::uuid`, prepared.RunID, completedAt); err != nil {
			return research.ProcessResult{}, fmt.Errorf("mark completed research obsolete: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return research.ProcessResult{}, fmt.Errorf("commit obsolete research completion: %w", err)
		}
		return research.ProcessResult{RunID: prepared.RunID, ProviderID: completion.ProviderID, Obsolete: true}, nil
	}
	validatedOutput, err := json.Marshal(completion.Output)
	if err != nil {
		return research.ProcessResult{}, fmt.Errorf("encode validated research output: %w", err)
	}
	uncertainties, err := json.Marshal(completion.Output.Uncertainties)
	if err != nil {
		return research.ProcessResult{}, fmt.Errorf("encode research uncertainties: %w", err)
	}
	sourceIDs := make(map[string]string, len(completion.Sources))
	for _, source := range completion.Sources {
		var sourceID string
		if err := transaction.QueryRow(ctx, `
			insert into app.research_sources (ai_run_id, source_url, source_domain)
			values ($1::uuid, $2, $3)
			on conflict (ai_run_id, source_url) do update set source_domain = excluded.source_domain
			returning id::text`, prepared.RunID, source.URL, source.Domain).Scan(&sourceID); err != nil {
			return research.ProcessResult{}, fmt.Errorf("persist returned research source: %w", err)
		}
		sourceIDs[source.URL] = sourceID
	}
	var briefID string
	if err := transaction.QueryRow(ctx, `
		insert into app.research_briefs (
			cluster_id, ai_run_id, headline, summary, why_it_matters,
			recommended_action, confidence, uncertainties
		) values ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8::jsonb)
		returning id::text`,
		prepared.ClusterID,
		prepared.RunID,
		completion.Output.Headline,
		completion.Output.Summary,
		completion.Output.WhyItMatters,
		completion.Output.RecommendedAction,
		completion.Output.Confidence,
		uncertainties,
	).Scan(&briefID); err != nil {
		return research.ProcessResult{}, fmt.Errorf("persist research brief: %w", err)
	}
	for index, assertion := range completion.Output.Assertions {
		var assertionID string
		if err := transaction.QueryRow(ctx, `
			insert into app.research_assertions (
				research_brief_id, assertion_index, assertion_text, material
			) values ($1::uuid, $2, $3, $4)
			returning id::text`, briefID, index, assertion.Text, assertion.Material).Scan(&assertionID); err != nil {
			return research.ProcessResult{}, fmt.Errorf("persist research assertion: %w", err)
		}
		for _, claimID := range assertion.ClaimIDs {
			commandTag, err := transaction.Exec(ctx, `
				insert into app.research_assertion_claims (research_assertion_id, claim_id)
				select $1::uuid, claim.id
				from app.claims claim
				join app.cluster_members member on member.item_id = claim.item_id
				where claim.id = $2::uuid and member.cluster_id = $3::uuid
					and claim.verification_state = 'verified_span'`, assertionID, claimID, prepared.ClusterID)
			if err != nil {
				return research.ProcessResult{}, fmt.Errorf("persist research claim reference: %w", err)
			}
			if commandTag.RowsAffected() != 1 {
				return research.ProcessResult{}, fmt.Errorf("research assertion references unavailable claim %q", claimID)
			}
		}
		for _, sourceURL := range assertion.SourceURLs {
			sourceID, exists := sourceIDs[sourceURL]
			if !exists {
				return research.ProcessResult{}, fmt.Errorf("research assertion references unavailable source %q", sourceURL)
			}
			if _, err := transaction.Exec(ctx, `
				insert into app.research_assertion_sources (research_assertion_id, research_source_id)
				values ($1::uuid, $2::uuid)`, assertionID, sourceID); err != nil {
				return research.ProcessResult{}, fmt.Errorf("persist research source reference: %w", err)
			}
		}
	}
	if _, err := transaction.Exec(ctx, `
		update app.ai_runs
		set provider_response_id = $2, state = 'completed', validated_output = $3::jsonb,
			completed_at = $4, error_code = null, updated_at = $4
		where id = $1::uuid`, prepared.RunID, completion.ProviderID, validatedOutput, completedAt); err != nil {
		return research.ProcessResult{}, fmt.Errorf("complete research run: %w", err)
	}
	if err := refreshRunTotals(ctx, transaction, prepared.RunID, completion.ProviderID, completedAt); err != nil {
		return research.ProcessResult{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return research.ProcessResult{}, fmt.Errorf("commit research completion: %w", err)
	}
	return research.ProcessResult{
		RunID: prepared.RunID, ProviderID: completion.ProviderID,
		AssertionCount: len(completion.Output.Assertions),
	}, nil
}

func (store *Store) Fail(ctx context.Context, prepared research.PreparedRun, errorCode string, failedAt time.Time) error {
	if errorCode == "" || failedAt.IsZero() {
		return errors.New("research failure requires an error code and timestamp")
	}
	state := "needs_review"
	var completedAt *time.Time
	if errorCode == "provider_error" || errorCode == "schema_invalid_retryable" {
		state = "failed_retryable"
	} else {
		completedAt = &failedAt
	}
	_, err := store.pool.Exec(ctx, `
		update app.ai_runs
		set state = $2, completed_at = $3, error_code = $4, updated_at = $5
		where id = $1::uuid and state not in ('completed', 'obsolete', 'budget_blocked')`,
		prepared.RunID,
		state,
		completedAt,
		errorCode,
		failedAt,
	)
	if err != nil {
		return fmt.Errorf("persist research failure: %w", err)
	}
	return nil
}

func selectEnabledRegistry(ctx context.Context, transaction pgx.Tx, prepared *research.PreparedRun) error {
	err := transaction.QueryRow(ctx, `
		select id::text, model_id, reasoning, verbosity, enabled_tools, max_output_tokens
		from app.model_configs
		where role = 'research' and provider = 'openai' and enabled
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
		return fmt.Errorf("%w: no enabled research model config", research.ErrConfigurationDrift)
	}
	if err != nil {
		return fmt.Errorf("select research model config: %w", err)
	}
	err = transaction.QueryRow(ctx, `
		select id::text, semantic_version, prompt_sha256, schema_version, schema_sha256
		from app.prompt_versions
		where purpose = $1 and active
		order by created_at desc, id desc
		limit 1`, research.Purpose).Scan(
		&prepared.Prompt.ID,
		&prepared.Prompt.SemanticVersion,
		&prepared.Prompt.PromptSHA256,
		&prepared.Prompt.SchemaVersion,
		&prepared.Prompt.SchemaSHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: no active research prompt", research.ErrConfigurationDrift)
	}
	if err != nil {
		return fmt.Errorf("select research prompt: %w", err)
	}
	return nil
}

func selectClaims(ctx context.Context, transaction pgx.Tx, clusterID string) ([]research.ClaimFact, error) {
	rows, err := transaction.Query(ctx, `
		select
			claim.id::text, claim.item_id::text, claim.revision_id::text,
			claim.claim_type, claim.claim_text, claim.normalized_value #>> '{}',
			claim.confidence, claim.material, claim.verification_state,
			source.canonical_url, source.source_tier
		from app.cluster_members member
		join app.items item on item.id = member.item_id
		join app.claims claim
			on claim.item_id = item.id and claim.revision_id = item.current_revision_id
		join app.item_sources source
			on source.item_id = item.id and source.revision_id = item.current_revision_id
		where member.cluster_id = $1::uuid
			and claim.verification_state = 'verified_span'
			and source.source_tier in ('T0', 'T1')
		order by source.source_tier, claim.material desc, claim.id
		limit $2`, clusterID, research.MaximumClaims)
	if err != nil {
		return nil, fmt.Errorf("select validated research claims: %w", err)
	}
	defer rows.Close()
	claims := make([]research.ClaimFact, 0)
	for rows.Next() {
		var claim research.ClaimFact
		if err := rows.Scan(
			&claim.ID,
			&claim.ItemID,
			&claim.RevisionID,
			&claim.ClaimType,
			&claim.ClaimText,
			&claim.NormalizedValue,
			&claim.Confidence,
			&claim.Material,
			&claim.VerificationState,
			&claim.SourceURL,
			&claim.SourceTier,
		); err != nil {
			return nil, fmt.Errorf("scan validated research claim: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate validated research claims: %w", err)
	}
	if len(claims) == 0 {
		return nil, fmt.Errorf("%w: story cluster has no evidence-verified T0/T1 claims", research.ErrInvalidTarget)
	}
	return claims, nil
}

func refreshRunTotals(ctx context.Context, transaction pgx.Tx, runID string, providerID string, updatedAt time.Time) error {
	_, err := transaction.Exec(ctx, `
		update app.ai_runs run
		set provider_response_id = coalesce(nullif($2, ''), run.provider_response_id),
			input_tokens = totals.input_tokens,
			cached_input_tokens = totals.cached_input_tokens,
			output_tokens = totals.output_tokens,
			tool_calls = totals.tool_calls,
			estimated_cost_usd = totals.estimated_cost_usd,
			updated_at = $3
		from (
			select ai_run_id, sum(input_tokens)::bigint as input_tokens,
				sum(cached_input_tokens)::bigint as cached_input_tokens,
				sum(output_tokens)::bigint as output_tokens,
				sum(tool_calls)::integer as tool_calls,
				sum(estimated_cost_usd)::numeric(14,8) as estimated_cost_usd
			from app.ai_run_attempts where ai_run_id = $1::uuid group by ai_run_id
		) totals
		where run.id = totals.ai_run_id`, runID, providerID, updatedAt)
	if err != nil {
		return fmt.Errorf("refresh research run totals: %w", err)
	}
	return nil
}
