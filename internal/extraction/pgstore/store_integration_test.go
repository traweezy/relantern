package pgstore_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/extraction/pgstore"
)

type integrationReader struct {
	objects map[string][]byte
}

func (reader integrationReader) Read(_ context.Context, objectKey string, maximumBytes int64) ([]byte, error) {
	payload, exists := reader.objects[objectKey]
	if !exists {
		return nil, errors.New("fixture object does not exist")
	}
	if int64(len(payload)) > maximumBytes {
		return nil, errors.New("fixture object exceeds bound")
	}
	return append([]byte(nil), payload...), nil
}

type integrationProvider struct {
	output extraction.Output
	calls  int
}

type failingIntegrationProvider struct {
	calls int
	err   error
}

type responseIntegrationProvider struct {
	calls     int
	responses []extraction.ProviderResponse
}

func (provider *failingIntegrationProvider) Extract(_ context.Context, _ extraction.ProviderRequest) (extraction.ProviderResponse, error) {
	provider.calls++
	return extraction.ProviderResponse{}, provider.err
}

func (provider *integrationProvider) Extract(_ context.Context, _ extraction.ProviderRequest) (extraction.ProviderResponse, error) {
	provider.calls++
	encoded, err := json.Marshal(provider.output)
	if err != nil {
		return extraction.ProviderResponse{}, err
	}
	return extraction.ProviderResponse{
		ID: fmt.Sprintf("resp_integration_%d", provider.calls), Output: string(encoded),
		Usage: extraction.Usage{InputTokens: 200, CachedInputTokens: 40, OutputTokens: 80},
	}, nil
}

func (provider *responseIntegrationProvider) Extract(_ context.Context, _ extraction.ProviderRequest) (extraction.ProviderResponse, error) {
	provider.calls++
	if provider.calls > len(provider.responses) {
		return extraction.ProviderResponse{}, errors.New("fixture provider exhausted responses")
	}
	return provider.responses[provider.calls-1], nil
}

type extractionFixture struct {
	SourceID   string
	ItemID     string
	RevisionID string
	ObjectKey  string
	Content    []byte
}

func TestStorePersistsIdempotentClaimsEvidenceUsageAndReviewPolicy(t *testing.T) {
	pool := openIntegrationDatabase(t)
	now := time.Date(2042, time.April, 15, 12, 0, 0, 0, time.UTC)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("pgstore.New() error = %v", err)
	}

	tests := []struct {
		name        string
		tier        string
		needsReview bool
	}{
		{name: "primary evidence", tier: "T0", needsReview: false},
		{name: "community material claim", tier: "T2", needsReview: true},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := insertExtractionFixture(t, pool, now.Add(time.Duration(index)*time.Hour), test.tier)
			provider := &integrationProvider{output: extraction.Output{
				TopicIDs: []string{"go"}, EventType: "release", LifecycleState: "stable",
				Claims: []extraction.Claim{{
					ClaimType: "version", ClaimText: "Go 1.27 is the released version.",
					NormalizedValue: "1.27", Confidence: "high", Material: true,
					EvidenceSpanIDs: []string{"span_0001"},
				}},
				Uncertainties: []string{},
			}}
			processor, err := extraction.NewProcessor(
				store,
				integrationReader{objects: map[string][]byte{fixture.ObjectKey: fixture.Content}},
				provider,
				clock.NewFixed(now.Add(2*time.Hour)),
				extraction.Config{
					ExpectedModelID: extraction.DefaultFastModelID, ExpectedReasoning: extraction.DefaultFastReasoning,
					ExpectedVerbosity:       extraction.DefaultVerbosity,
					ExpectedMaxOutputTokens: extraction.DefaultMaximumOutputTokens,
					MonthlySoftUSD:          "25.00", MonthlyHardUSD: "50.00", MaximumAge: 24 * time.Hour,
				},
			)
			if err != nil {
				t.Fatalf("extraction.NewProcessor() error = %v", err)
			}
			request := extraction.ProcessRequest{ItemID: fixture.ItemID, RevisionID: fixture.RevisionID}
			result, err := processor.Process(context.Background(), request)
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			if result.ClaimCount != 1 || result.NeedsReview != test.needsReview || provider.calls != 1 {
				t.Fatalf("result = %+v, provider calls = %d", result, provider.calls)
			}
			assertPersistedExtraction(t, pool, fixture, result.RunID, test.needsReview)

			replayed, err := processor.Process(context.Background(), request)
			if err != nil {
				t.Fatalf("replayed Process() error = %v", err)
			}
			if !replayed.AlreadyCompleted || replayed.RunID != result.RunID || provider.calls != 1 {
				t.Fatalf("replayed result = %+v, provider calls = %d", replayed, provider.calls)
			}
			terminal, err := store.Prepare(context.Background(), request, now.Add(3*time.Hour))
			if err != nil {
				t.Fatalf("terminal Prepare() error = %v", err)
			}
			duplicate, err := store.Complete(context.Background(), terminal, extraction.Completion{
				CompletedState: terminal.State,
			}, now.Add(4*time.Hour))
			if err != nil || !duplicate.AlreadyCompleted || duplicate.ClaimCount != 1 {
				t.Fatalf("terminal Complete() = %+v, %v", duplicate, err)
			}
			if _, err := store.ReserveAttempt(context.Background(), terminal, 100, "25.00", "50.00", now.Add(4*time.Hour)); err == nil {
				t.Fatal("ReserveAttempt() accepted a terminal run")
			}
		})
	}
}

func TestStorePersistsRetryableAndIntegrityFailures(t *testing.T) {
	pool := openIntegrationDatabase(t)
	now := time.Date(2044, time.June, 10, 12, 0, 0, 0, time.UTC)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("provider failure is retryable and conservatively billed", func(t *testing.T) {
		fixture := insertExtractionFixture(t, pool, now, "T0")
		providerError := errors.New("fixture provider unavailable")
		provider := &failingIntegrationProvider{err: providerError}
		processor := newIntegrationProcessor(
			t,
			store,
			integrationReader{objects: map[string][]byte{fixture.ObjectKey: fixture.Content}},
			provider,
			now.Add(time.Hour),
			"0.00000001",
			"50.00",
		)
		_, err := processor.Process(context.Background(), extraction.ProcessRequest{ItemID: fixture.ItemID, RevisionID: fixture.RevisionID})
		if !errors.Is(err, providerError) || provider.calls != 1 {
			t.Fatalf("Process() error = %v, calls = %d", err, provider.calls)
		}
		var runState string
		var itemState string
		var attemptState string
		var softAlert bool
		var reservedCost string
		var actualCost string
		if err := pool.QueryRow(context.Background(), `
			select run.state, item.lifecycle_state, attempt.state,
				attempt.budget_soft_alert, attempt.reserved_cost_usd::text,
				attempt.estimated_cost_usd::text
			from app.ai_runs run
			join app.items item on item.id = run.item_id
			join app.ai_run_attempts attempt on attempt.ai_run_id = run.id
			where run.revision_id = $1::uuid`, fixture.RevisionID).Scan(
			&runState, &itemState, &attemptState, &softAlert, &reservedCost, &actualCost,
		); err != nil {
			t.Fatalf("inspect retryable extraction failure: %v", err)
		}
		if runState != "failed_retryable" || itemState != "failed_retryable" ||
			attemptState != "failed" || !softAlert || reservedCost == "0.00000000" || actualCost != reservedCost {
			t.Fatalf("failure state = %q/%q/%q, soft = %t, cost = %s/%s",
				runState, itemState, attemptState, softAlert, reservedCost, actualCost)
		}
	})

	t.Run("revision integrity failure requires review without provider use", func(t *testing.T) {
		fixture := insertExtractionFixture(t, pool, now.Add(2*time.Hour), "T0")
		provider := &integrationProvider{}
		corrupt := append([]byte(nil), fixture.Content...)
		corrupt[0] = 'X'
		processor := newIntegrationProcessor(
			t,
			store,
			integrationReader{objects: map[string][]byte{fixture.ObjectKey: corrupt}},
			provider,
			now.Add(3*time.Hour),
			"25.00",
			"50.00",
		)
		_, err := processor.Process(context.Background(), extraction.ProcessRequest{ItemID: fixture.ItemID, RevisionID: fixture.RevisionID})
		if !errors.Is(err, extraction.ErrRevisionIntegrity) || provider.calls != 0 {
			t.Fatalf("Process() error = %v, provider calls = %d", err, provider.calls)
		}
		var runState string
		var itemState string
		var errorCode string
		var attemptCount int
		if err := pool.QueryRow(context.Background(), `
			select run.state, item.lifecycle_state, run.error_code, count(attempt.id)
			from app.ai_runs run
			join app.items item on item.id = run.item_id
			left join app.ai_run_attempts attempt on attempt.ai_run_id = run.id
			where run.revision_id = $1::uuid
			group by run.id, item.id`, fixture.RevisionID).Scan(&runState, &itemState, &errorCode, &attemptCount); err != nil {
			t.Fatalf("inspect integrity extraction failure: %v", err)
		}
		if runState != "needs_review" || itemState != "needs_review" || errorCode != "revision_integrity" || attemptCount != 0 {
			t.Fatalf("integrity state = %q/%q/%q, attempts = %d", runState, itemState, errorCode, attemptCount)
		}
	})

	t.Run("schema failure is reviewed after the bounded retry", func(t *testing.T) {
		fixture := insertExtractionFixture(t, pool, now.Add(4*time.Hour), "T0")
		provider := &responseIntegrationProvider{responses: []extraction.ProviderResponse{
			{ID: "resp_schema_1", Output: `{}`, Usage: extraction.Usage{InputTokens: 120, OutputTokens: 20}},
			{ID: "resp_schema_2", Output: `{}`, Usage: extraction.Usage{InputTokens: 120, OutputTokens: 20}},
		}}
		processor := newIntegrationProcessor(
			t,
			store,
			integrationReader{objects: map[string][]byte{fixture.ObjectKey: fixture.Content}},
			provider,
			now.Add(5*time.Hour),
			"25.00",
			"50.00",
		)
		_, err := processor.Process(context.Background(), extraction.ProcessRequest{ItemID: fixture.ItemID, RevisionID: fixture.RevisionID})
		if !errors.Is(err, extraction.ErrSchemaInvalid) || provider.calls != extraction.MaximumSchemaAttempts {
			t.Fatalf("Process() error = %v, calls = %d", err, provider.calls)
		}
		var runState string
		var itemState string
		var attemptCount int
		var schemaFailureCount int
		if err := pool.QueryRow(context.Background(), `
			select run.state, item.lifecycle_state, count(attempt.id),
				count(attempt.id) filter (
					where attempt.state = 'failed' and attempt.error_code = 'schema_invalid'
				)
			from app.ai_runs run
			join app.items item on item.id = run.item_id
			join app.ai_run_attempts attempt on attempt.ai_run_id = run.id
			where run.revision_id = $1::uuid
			group by run.id, item.id`, fixture.RevisionID).Scan(
			&runState,
			&itemState,
			&attemptCount,
			&schemaFailureCount,
		); err != nil {
			t.Fatalf("inspect schema extraction failure: %v", err)
		}
		if runState != "needs_review" || itemState != "needs_review" ||
			attemptCount != extraction.MaximumSchemaAttempts || schemaFailureCount != extraction.MaximumSchemaAttempts {
			t.Fatalf("schema state = %q/%q, attempts = %d/%d",
				runState, itemState, attemptCount, schemaFailureCount)
		}
	})
}

func TestStoreHandlesObsoleteInterruptedAndInvalidEvidenceRuns(t *testing.T) {
	pool := openIntegrationDatabase(t)
	now := time.Date(2045, time.July, 10, 12, 0, 0, 0, time.UTC)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("prepare recognizes an obsolete requested revision", func(t *testing.T) {
		fixture := insertExtractionFixture(t, pool, now, "T0")
		prepared, err := store.Prepare(context.Background(), extraction.ProcessRequest{
			ItemID: fixture.ItemID, RevisionID: uuid.NewString(),
		}, now.Add(time.Minute))
		if err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}
		if !prepared.Obsolete || prepared.State != "obsolete" || prepared.RunID != "" {
			t.Fatalf("obsolete preparation = %+v", prepared)
		}
	})

	t.Run("completion becomes obsolete after the current revision advances", func(t *testing.T) {
		fixture := insertExtractionFixture(t, pool, now.Add(time.Hour), "T0")
		prepared, err := store.Prepare(context.Background(), extraction.ProcessRequest{
			ItemID: fixture.ItemID, RevisionID: fixture.RevisionID,
		}, now.Add(time.Hour+time.Minute))
		if err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}
		advanceCurrentRevision(t, pool, fixture, now.Add(time.Hour+2*time.Minute))
		result, err := store.Complete(context.Background(), prepared, extraction.Completion{
			CompletedState: "completed",
		}, now.Add(time.Hour+3*time.Minute))
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if !result.Obsolete || result.RunID != prepared.RunID {
			t.Fatalf("obsolete completion = %+v", result)
		}
		var state string
		var errorCode string
		if err := pool.QueryRow(context.Background(), `
			select state, error_code from app.ai_runs where id = $1::uuid`, prepared.RunID).Scan(&state, &errorCode); err != nil {
			t.Fatalf("inspect obsolete extraction: %v", err)
		}
		if state != "obsolete" || errorCode != "obsolete_revision" {
			t.Fatalf("obsolete extraction state = %q, error = %q", state, errorCode)
		}
	})

	t.Run("a new reservation closes an interrupted attempt", func(t *testing.T) {
		fixture := insertExtractionFixture(t, pool, now.Add(2*time.Hour), "T0")
		prepared, err := store.Prepare(context.Background(), extraction.ProcessRequest{
			ItemID: fixture.ItemID, RevisionID: fixture.RevisionID,
		}, now.Add(2*time.Hour+time.Minute))
		if err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}
		first, err := store.ReserveAttempt(context.Background(), prepared, 100, "25.00", "50.00", now.Add(2*time.Hour+2*time.Minute))
		if err != nil {
			t.Fatalf("first ReserveAttempt() error = %v", err)
		}
		second, err := store.ReserveAttempt(context.Background(), prepared, 100, "25.00", "50.00", now.Add(2*time.Hour+3*time.Minute))
		if err != nil {
			t.Fatalf("second ReserveAttempt() error = %v", err)
		}
		if first.Number != 1 || second.Number != 2 {
			t.Fatalf("reservation numbers = %d, %d", first.Number, second.Number)
		}
		if err := store.RecordAttempt(context.Background(), prepared, first, extraction.ProviderResponse{}, "", now.Add(2*time.Hour+4*time.Minute)); err == nil {
			t.Fatal("RecordAttempt() accepted a closed interrupted reservation")
		}
		if err := store.RecordAttempt(context.Background(), prepared, second, extraction.ProviderResponse{
			ID: "resp_after_interrupt", Usage: extraction.Usage{InputTokens: 80, CachedInputTokens: 20, OutputTokens: 10},
		}, "", now.Add(2*time.Hour+4*time.Minute)); err != nil {
			t.Fatalf("RecordAttempt() error = %v", err)
		}
		var state string
		var errorCode string
		var reservedCost string
		var actualCost string
		if err := pool.QueryRow(context.Background(), `
			select state, error_code, reserved_cost_usd::text, estimated_cost_usd::text
			from app.ai_run_attempts where id = $1`, first.ID).Scan(&state, &errorCode, &reservedCost, &actualCost); err != nil {
			t.Fatalf("inspect interrupted attempt: %v", err)
		}
		if state != "failed" || errorCode != "worker_interrupted" || actualCost != reservedCost {
			t.Fatalf("interrupted attempt = %q/%q, cost = %s/%s", state, errorCode, actualCost, reservedCost)
		}
	})

	t.Run("unvalidated evidence rolls back the claim transaction", func(t *testing.T) {
		fixture := insertExtractionFixture(t, pool, now.Add(3*time.Hour), "T0")
		prepared, err := store.Prepare(context.Background(), extraction.ProcessRequest{
			ItemID: fixture.ItemID, RevisionID: fixture.RevisionID,
		}, now.Add(3*time.Hour+time.Minute))
		if err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}
		missingItem := prepared
		missingItem.ItemID = uuid.NewString()
		if _, err := store.Complete(context.Background(), missingItem, extraction.Completion{
			CompletedState: "completed",
		}, now.Add(3*time.Hour+2*time.Minute)); err == nil {
			t.Fatal("Complete() accepted a run whose item no longer exists")
		}
		_, err = store.Complete(context.Background(), prepared, extraction.Completion{
			CompletedState: "completed",
			Output: extraction.Output{Claims: []extraction.Claim{{
				ClaimType: "version", ClaimText: "unsupported fixture claim",
				NormalizedValue: "1.27", Confidence: "high", Material: true,
				EvidenceSpanIDs: []string{"span_missing"},
			}}},
		}, now.Add(3*time.Hour+2*time.Minute))
		if err == nil {
			t.Fatal("Complete() accepted an unvalidated evidence span")
		}
		var claimCount int
		if err := pool.QueryRow(context.Background(), `
			select count(*) from app.claims where ai_run_id = $1::uuid`, prepared.RunID).Scan(&claimCount); err != nil {
			t.Fatalf("count rolled-back claims: %v", err)
		}
		if claimCount != 0 {
			t.Fatalf("rolled-back claim count = %d, want 0", claimCount)
		}
	})
}

func TestStoreEnforcesHardBudgetBeforeProviderCall(t *testing.T) {
	pool := openIntegrationDatabase(t)
	now := time.Date(2043, time.May, 10, 12, 0, 0, 0, time.UTC)
	fixture := insertExtractionFixture(t, pool, now, "T0")
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.Prepare(
		context.Background(),
		extraction.ProcessRequest{ItemID: fixture.ItemID, RevisionID: fixture.RevisionID},
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	_, err = store.ReserveAttempt(context.Background(), prepared, 100, "0.00000001", "0.00000001", now.Add(2*time.Minute))
	if !errors.Is(err, extraction.ErrBudgetExceeded) {
		t.Fatalf("ReserveAttempt() error = %v, want ErrBudgetExceeded", err)
	}
	var state string
	var attemptCount int
	if err := pool.QueryRow(context.Background(), `
		select run.state, count(attempt.id)
		from app.ai_runs run
		left join app.ai_run_attempts attempt on attempt.ai_run_id = run.id
		where run.id = $1::uuid
		group by run.id`, prepared.RunID).Scan(&state, &attemptCount); err != nil {
		t.Fatalf("inspect budget-blocked run: %v", err)
	}
	if state != "budget_blocked" || attemptCount != 0 {
		t.Fatalf("budget-blocked state = %q, attempt count = %d", state, attemptCount)
	}
}

func TestStoreRejectsInvalidBoundaries(t *testing.T) {
	pool := openIntegrationDatabase(t)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pgstore.New(nil); err == nil {
		t.Fatal("pgstore.New(nil) succeeded")
	}
	if _, err := store.Prepare(context.Background(), extraction.ProcessRequest{}, time.Time{}); err == nil {
		t.Fatal("Prepare() accepted empty input")
	}
	if _, err := store.Prepare(context.Background(), extraction.ProcessRequest{
		ItemID: uuid.NewString(), RevisionID: uuid.NewString(),
	}, time.Now()); !errors.Is(err, extraction.ErrInvalidTarget) {
		t.Fatalf("Prepare() missing target error = %v", err)
	}
	if _, err := store.ReserveAttempt(context.Background(), extraction.PreparedRun{}, 0, "25", "50", time.Now()); err == nil {
		t.Fatal("ReserveAttempt() accepted zero estimated tokens")
	}
	if _, err := store.ReserveAttempt(context.Background(), extraction.PreparedRun{}, 100, "51", "50", time.Now()); err == nil {
		t.Fatal("ReserveAttempt() accepted an invalid budget range")
	}
	if _, err := store.ReserveAttempt(context.Background(), extraction.PreparedRun{RunID: uuid.NewString()}, 100, "25", "50", time.Now()); err == nil {
		t.Fatal("ReserveAttempt() accepted a missing run")
	}
	if err := store.RecordAttempt(context.Background(), extraction.PreparedRun{}, extraction.AttemptReservation{}, extraction.ProviderResponse{
		Usage: extraction.Usage{InputTokens: 1, CachedInputTokens: 2},
	}, "", time.Now()); err == nil {
		t.Fatal("RecordAttempt() accepted cached tokens exceeding input tokens")
	}
	if err := store.RecordAttempt(context.Background(), extraction.PreparedRun{RunID: "invalid"}, extraction.AttemptReservation{}, extraction.ProviderResponse{}, "", time.Now()); err == nil {
		t.Fatal("RecordAttempt() accepted an invalid run ID")
	}
	if _, err := store.Complete(context.Background(), extraction.PreparedRun{}, extraction.Completion{CompletedState: "invalid"}, time.Now()); err == nil {
		t.Fatal("Complete() accepted an invalid state")
	}
	if _, err := store.Complete(context.Background(), extraction.PreparedRun{RunID: uuid.NewString()}, extraction.Completion{CompletedState: "completed"}, time.Now()); err == nil {
		t.Fatal("Complete() accepted a missing run")
	}
	if err := store.Fail(context.Background(), extraction.PreparedRun{}, "", time.Now()); err == nil {
		t.Fatal("Fail() accepted an empty error code")
	}
	if err := store.Fail(context.Background(), extraction.PreparedRun{RunID: "invalid"}, "fixture_error", time.Now()); err == nil {
		t.Fatal("Fail() accepted an invalid run ID")
	}

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Prepare(canceledContext, extraction.ProcessRequest{ItemID: uuid.NewString(), RevisionID: uuid.NewString()}, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Prepare() canceled error = %v", err)
	}
	if _, err := store.ReserveAttempt(canceledContext, extraction.PreparedRun{}, 100, "25", "50", time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReserveAttempt() canceled error = %v", err)
	}
	if err := store.RecordAttempt(canceledContext, extraction.PreparedRun{}, extraction.AttemptReservation{}, extraction.ProviderResponse{}, "", time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("RecordAttempt() canceled error = %v", err)
	}
	if _, err := store.Complete(canceledContext, extraction.PreparedRun{}, extraction.Completion{CompletedState: "completed"}, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete() canceled error = %v", err)
	}
	if err := store.Fail(canceledContext, extraction.PreparedRun{}, "fixture_error", time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Fail() canceled error = %v", err)
	}
}

func newIntegrationProcessor(
	t *testing.T,
	store *pgstore.Store,
	reader integrationReader,
	provider extraction.Provider,
	now time.Time,
	softBudget extraction.USD,
	hardBudget extraction.USD,
) *extraction.Processor {
	t.Helper()
	processor, err := extraction.NewProcessor(
		store,
		reader,
		provider,
		clock.NewFixed(now),
		extraction.Config{
			ExpectedModelID: extraction.DefaultFastModelID, ExpectedReasoning: extraction.DefaultFastReasoning,
			ExpectedVerbosity:       extraction.DefaultVerbosity,
			ExpectedMaxOutputTokens: extraction.DefaultMaximumOutputTokens,
			MonthlySoftUSD:          softBudget, MonthlyHardUSD: hardBudget, MaximumAge: 24 * time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("extraction.NewProcessor() error = %v", err)
	}
	return processor
}

func assertPersistedExtraction(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture extractionFixture,
	runID string,
	needsReview bool,
) {
	t.Helper()
	wantRunState := "completed"
	wantItemState := "ready"
	wantVerification := "verified_span"
	if needsReview {
		wantRunState = "needs_review"
		wantItemState = "needs_review"
		wantVerification = "review_required"
	}
	var runState string
	var providerResponseID string
	var inputTokens int64
	var cachedTokens int64
	var outputTokens int64
	var cost string
	var validatedEvent string
	if err := pool.QueryRow(context.Background(), `
		select state, provider_response_id, input_tokens, cached_input_tokens,
			output_tokens, estimated_cost_usd::text, validated_output->>'event_type'
		from app.ai_runs where id = $1::uuid`, runID).Scan(
		&runState, &providerResponseID, &inputTokens, &cachedTokens, &outputTokens, &cost, &validatedEvent,
	); err != nil {
		t.Fatalf("select extraction run: %v", err)
	}
	if runState != wantRunState || providerResponseID != "resp_integration_1" || inputTokens != 200 ||
		cachedTokens != 40 || outputTokens != 80 || cost != "0.00012880" || validatedEvent != "release" {
		t.Fatalf("run state = %q, provider = %q, usage = %d/%d/%d, cost = %s, event = %q",
			runState, providerResponseID, inputTokens, cachedTokens, outputTokens, cost, validatedEvent)
	}
	var claimCount int
	var normalizedValue string
	var verificationState string
	var spanCount int
	var quoteHash []byte
	if err := pool.QueryRow(context.Background(), `
		select count(*) over (), claim.normalized_value #>> '{}', claim.verification_state,
			count(span.id) over (partition by claim.id), span.quote_hash
		from app.claims claim
		join app.evidence_spans span on span.claim_id = claim.id
		where claim.ai_run_id = $1::uuid`, runID).Scan(
		&claimCount, &normalizedValue, &verificationState, &spanCount, &quoteHash,
	); err != nil {
		t.Fatalf("select persisted claim evidence: %v", err)
	}
	expectedQuoteHash := sha256.Sum256(fixture.Content)
	if claimCount != 1 || normalizedValue != "1.27" || verificationState != wantVerification ||
		spanCount != 1 || string(quoteHash) != string(expectedQuoteHash[:]) {
		t.Fatalf("claim count = %d, normalized = %q, verification = %q, spans = %d, quote hash = %x",
			claimCount, normalizedValue, verificationState, spanCount, quoteHash)
	}
	var itemState string
	var eventType string
	if err := pool.QueryRow(context.Background(), `select lifecycle_state, event_type from app.items where id = $1::uuid`, fixture.ItemID).Scan(&itemState, &eventType); err != nil {
		t.Fatalf("select extracted item: %v", err)
	}
	if itemState != wantItemState || eventType != "release" {
		t.Fatalf("item state = %q, event = %q", itemState, eventType)
	}
}

func insertExtractionFixture(t *testing.T, pool *pgxpool.Pool, now time.Time, tier string) extractionFixture {
	t.Helper()
	fixtureID := uuid.NewString()
	sourceID := "extraction-" + fixtureID
	content := []byte("Go 1.27 is the stable released version.")
	digest := sha256.Sum256(content)
	fixture := extractionFixture{
		SourceID:  sourceID,
		ObjectKey: fmt.Sprintf("normalized/%s/%x.txt", sourceID, digest),
		Content:   content,
	}
	if err := pool.QueryRow(context.Background(), `
		insert into app.sources (
			id, name, trust_tier, owner, origin, validation_state, homepage_url,
			content_policy, enabled, topics, reviewed_at
		) values ($1, $2, $3, 'integration', 'system', 'active', $4, 'link-and-excerpt', true, array['go'], $5)
		returning id`, sourceID, "Extraction fixture "+fixtureID, tier, "https://example.test/"+fixtureID, now).Scan(&fixture.SourceID); err != nil {
		t.Fatalf("insert extraction source: %v", err)
	}
	var rawDocumentID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256, first_seen_at,
			first_fetched_at, content_policy
		) values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt')
		returning id::text`,
		sourceID,
		"https://example.test/"+fixtureID+"/release",
		fmt.Sprintf("raw/%s/2042/04/15/%x.txt", sourceID, digest),
		digest[:],
		now,
	).Scan(&rawDocumentID); err != nil {
		t.Fatalf("insert extraction raw document: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `
		insert into app.content_revisions (
			raw_document_id, normalized_sha256, normalized_text_object_key,
			parser_name, parser_version, title, language, normalized_bytes,
			outline, offset_map, warnings, change_kind, change_reason,
			material_change, observed_at
		) values (
			$1::uuid, $2, $3, 'fixture', '1.0.0', 'Go 1.27 release', 'en', $4,
			'[]'::jsonb, '[]'::jsonb, '[]'::jsonb, 'initial', 'integration fixture', false, $5
		) returning id::text`, rawDocumentID, digest[:], fixture.ObjectKey, len(content), now).Scan(&fixture.RevisionID); err != nil {
		t.Fatalf("insert extraction revision: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `
		insert into app.items (
			current_revision_id, canonical_url, title, normalized_title, normalized_author,
			package_name, version, slug, lifecycle_state, first_seen_at, status, simhash
		) values (
			$1::uuid, $2, 'Go 1.27 release', 'go 1.27 release', 'go team',
			'go', '1.27', $3, 'awaiting_ai', $4, 'active', $5
		) returning id::text`,
		fixture.RevisionID,
		"https://example.test/"+fixtureID+"/release",
		"go-release-"+fixtureID,
		now,
		make([]byte, 8),
	).Scan(&fixture.ItemID); err != nil {
		t.Fatalf("insert extraction item: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into app.item_sources (
			revision_id, item_id, canonical_url, source_role, source_tier, sort_order
		) values ($1::uuid, $2::uuid, $3, 'primary', $4, 0)`,
		fixture.RevisionID,
		fixture.ItemID,
		"https://example.test/"+fixtureID+"/release",
		tier,
	); err != nil {
		t.Fatalf("insert extraction item source: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `delete from app.evidence_spans where revision_id = $1::uuid`, fixture.RevisionID)
		_, _ = pool.Exec(ctx, `delete from app.claims where revision_id = $1::uuid`, fixture.RevisionID)
		_, _ = pool.Exec(ctx, `delete from app.ai_run_attempts where ai_run_id in (select id from app.ai_runs where revision_id = $1::uuid)`, fixture.RevisionID)
		_, _ = pool.Exec(ctx, `delete from app.ai_runs where revision_id = $1::uuid`, fixture.RevisionID)
		_, _ = pool.Exec(ctx, `delete from app.item_sources where revision_id = $1::uuid`, fixture.RevisionID)
		_, _ = pool.Exec(ctx, `delete from app.items where id = $1::uuid`, fixture.ItemID)
		_, _ = pool.Exec(ctx, `delete from app.content_revisions where id = $1::uuid`, fixture.RevisionID)
		_, _ = pool.Exec(ctx, `delete from app.raw_documents where id = $1::uuid`, rawDocumentID)
		_, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, sourceID)
	})
	return fixture
}

func advanceCurrentRevision(t *testing.T, pool *pgxpool.Pool, fixture extractionFixture, observedAt time.Time) {
	t.Helper()
	content := []byte("Go 1.27.1 is the current stable released version.")
	digest := sha256.Sum256(content)
	var revisionID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.content_revisions (
			raw_document_id, previous_revision_id, normalized_sha256,
			normalized_text_object_key, parser_name, parser_version, title,
			language, normalized_bytes, outline, offset_map, warnings,
			change_kind, change_reason, material_change, observed_at
		) select
			raw_document_id, id, $2, $3, 'fixture', '1.0.0',
			'Go 1.27.1 release', 'en', $4, '[]'::jsonb, '[]'::jsonb,
			'[]'::jsonb, 'material', 'integration fixture update', true, $5
		from app.content_revisions where id = $1::uuid
		returning id::text`,
		fixture.RevisionID,
		digest[:],
		fmt.Sprintf("normalized/%s/%x.txt", fixture.SourceID, digest),
		len(content),
		observedAt,
	).Scan(&revisionID); err != nil {
		t.Fatalf("insert advanced extraction revision: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		update app.items set current_revision_id = $2::uuid where id = $1::uuid`, fixture.ItemID, revisionID); err != nil {
		t.Fatalf("advance current extraction revision: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `update app.items set current_revision_id = $2::uuid where id = $1::uuid`, fixture.ItemID, fixture.RevisionID)
		_, _ = pool.Exec(ctx, `delete from app.content_revisions where id = $1::uuid`, revisionID)
	})
}

func openIntegrationDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for structured extraction integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("config.LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
