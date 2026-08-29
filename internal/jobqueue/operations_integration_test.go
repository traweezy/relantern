package jobqueue_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func TestManualRetryIsAtomicAndAudited(t *testing.T) {
	pool := openJobQueueIntegrationPool(t)
	client, err := river.NewClient(
		riverpgxv5.New(pool),
		&river.Config{Schema: jobqueue.Schema},
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	inserted, err := client.Insert(
		context.Background(),
		jobqueue.ScheduleOccurrenceArgs{OccurrenceID: uuid.NewString()},
		nil,
	)
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	jobID := inserted.Job.ID
	jobIDText := fmt.Sprintf("%d", jobID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			delete from app.audit_events
			where target_type = 'river_job' and target_id = $1`, jobIDText)
		_, _ = pool.Exec(context.Background(), "delete from river.river_job where id = $1", jobID)
	})
	if _, err := pool.Exec(context.Background(), `
		update river.river_job
		set state = 'discarded', attempt = max_attempts, finalized_at = now()
		where id = $1`, jobID); err != nil {
		t.Fatalf("mark job discarded: %v", err)
	}

	operations := jobqueue.NewOperations(pool, client)
	deadLetters, err := operations.ListDeadLetters(context.Background(), 200)
	if err != nil {
		t.Fatalf("ListDeadLetters() error = %v", err)
	}
	found := false
	for _, deadLetter := range deadLetters {
		if deadLetter.ID == jobID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("discarded job %d was not visible", jobID)
	}

	retried, err := operations.Retry(context.Background(), jobqueue.RetryRequest{
		JobID:     jobID,
		ActorType: "owner",
		ActorID:   "integration-owner",
		RequestID: "integration-request",
		Reason:    "validate audited retry behavior",
	})
	if err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if retried.State != rivertype.JobStateAvailable || retried.MaxAttempts != inserted.Job.MaxAttempts+1 {
		t.Fatalf("retried job state = %s, max attempts = %d", retried.State, retried.MaxAttempts)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `
		select count(*)
		from app.audit_events
		where action = 'job.manual_retry'
			and target_type = 'river_job'
			and target_id = $1`, jobIDText).Scan(&auditCount); err != nil {
		t.Fatalf("inspect retry audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("retry audit count = %d, want 1", auditCount)
	}
	if _, err := operations.Retry(context.Background(), jobqueue.RetryRequest{
		JobID:     jobID,
		ActorType: "owner",
		ActorID:   "integration-owner",
		Reason:    "should not duplicate an available job",
	}); !errors.Is(err, jobqueue.ErrJobNotRetryable) {
		t.Fatalf("second Retry() error = %v, want ErrJobNotRetryable", err)
	}
}

func TestTransactionalInserterSuppressesDuplicateOccurrenceJob(t *testing.T) {
	pool := openJobQueueIntegrationPool(t)
	inserter, err := jobqueue.NewInserter()
	if err != nil {
		t.Fatalf("NewInserter() error = %v", err)
	}
	tx, err := pool.BeginTx(context.Background(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	occurrenceID := uuid.NewString()
	firstJobID, firstInserted, err := inserter.EnqueueScheduleOccurrence(
		context.Background(),
		tx,
		occurrenceID,
		"first-run",
	)
	if err != nil {
		t.Fatalf("first EnqueueScheduleOccurrence() error = %v", err)
	}
	secondJobID, secondInserted, err := inserter.EnqueueScheduleOccurrence(
		context.Background(),
		tx,
		occurrenceID,
		"second-run",
	)
	if err != nil {
		t.Fatalf("second EnqueueScheduleOccurrence() error = %v", err)
	}
	if !firstInserted || secondInserted || firstJobID != secondJobID {
		t.Fatalf(
			"duplicate results = first (%d, %t), second (%d, %t)",
			firstJobID,
			firstInserted,
			secondJobID,
			secondInserted,
		)
	}
}

func TestTransactionalInserterSuppressesDuplicateReembeddingJob(t *testing.T) {
	pool := openJobQueueIntegrationPool(t)
	inserter, err := jobqueue.NewInserter()
	if err != nil {
		t.Fatalf("NewInserter() error = %v", err)
	}
	tx, err := pool.BeginTx(context.Background(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	arguments := jobqueue.ReembedEntityArgs{
		EntityType: "item",
		EntityID:   uuid.NewString(),
		RevisionID: uuid.NewString(),
		ModelID:    "text-embedding-3-small",
	}
	firstJobID, firstInserted, err := inserter.EnqueueReembedEntity(context.Background(), tx, arguments)
	if err != nil {
		t.Fatalf("first EnqueueReembedEntity() error = %v", err)
	}
	secondJobID, secondInserted, err := inserter.EnqueueReembedEntity(context.Background(), tx, arguments)
	if err != nil {
		t.Fatalf("second EnqueueReembedEntity() error = %v", err)
	}
	if !firstInserted || secondInserted || firstJobID != secondJobID {
		t.Fatalf(
			"duplicate results = first (%d, %t), second (%d, %t)",
			firstJobID,
			firstInserted,
			secondJobID,
			secondInserted,
		)
	}
}

func TestTransactionalInserterSuppressesDuplicateExtractionJob(t *testing.T) {
	pool := openJobQueueIntegrationPool(t)
	inserter, err := jobqueue.NewInserter()
	if err != nil {
		t.Fatalf("NewInserter() error = %v", err)
	}
	tx, err := pool.BeginTx(context.Background(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	arguments := jobqueue.ExtractItemArgs{ItemID: uuid.NewString(), RevisionID: uuid.NewString()}
	firstJobID, firstInserted, err := inserter.EnqueueExtractItem(context.Background(), tx, arguments)
	if err != nil {
		t.Fatalf("first EnqueueExtractItem() error = %v", err)
	}
	secondJobID, secondInserted, err := inserter.EnqueueExtractItem(context.Background(), tx, arguments)
	if err != nil {
		t.Fatalf("second EnqueueExtractItem() error = %v", err)
	}
	if !firstInserted || secondInserted || firstJobID != secondJobID {
		t.Fatalf("duplicate results = first (%d, %t), second (%d, %t)", firstJobID, firstInserted, secondJobID, secondInserted)
	}
}

func TestOperationsRejectInvalidBoundaries(t *testing.T) {
	pool := openJobQueueIntegrationPool(t)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: jobqueue.Schema})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	operations := jobqueue.NewOperations(pool, client)
	if _, err := operations.ListDeadLetters(context.Background(), 0); err == nil {
		t.Fatal("ListDeadLetters() accepted a zero limit")
	}
	if _, err := operations.Retry(context.Background(), jobqueue.RetryRequest{}); !errors.Is(err, jobqueue.ErrRetryAuditInput) {
		t.Fatalf("Retry() error = %v, want ErrRetryAuditInput", err)
	}
	if _, err := operations.Retry(context.Background(), jobqueue.RetryRequest{
		JobID:     9_223_372_036_854_000_000,
		ActorType: "owner",
		ActorID:   "integration-owner",
		Reason:    "validate missing job handling",
	}); !errors.Is(err, rivertype.ErrNotFound) {
		t.Fatalf("missing Retry() error = %v, want ErrNotFound", err)
	}
}

func openJobQueueIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for job queue integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
