package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func runJobOperation(arguments []string, logger *slog.Logger) error {
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		return fmt.Errorf("load database configuration: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, databaseConfig)
	if err != nil {
		return err
	}
	defer pool.Close()
	client, err := river.NewClient(
		riverpgxv5.New(pool),
		&river.Config{Schema: jobqueue.Schema},
	)
	if err != nil {
		return fmt.Errorf("create River operations client: %w", err)
	}
	operations := jobqueue.NewOperations(pool, client)

	switch arguments[0] {
	case "dead-letters":
		return listDeadLetters(ctx, arguments[1:], operations)
	case "retry-job":
		return retryJob(ctx, arguments[1:], logger, operations)
	default:
		return errors.New("unsupported job operation")
	}
}

func listDeadLetters(
	ctx context.Context,
	arguments []string,
	operations *jobqueue.Operations,
) error {
	flags := flag.NewFlagSet("dead-letters", flag.ContinueOnError)
	limit := flags.Int("limit", 50, "maximum discarded jobs to display (1-200)")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	deadLetters, err := operations.ListDeadLetters(ctx, *limit)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"count": len(deadLetters),
		"jobs":  deadLetters,
	}); err != nil {
		return fmt.Errorf("encode dead-letter list: %w", err)
	}
	return nil
}

func retryJob(
	ctx context.Context,
	arguments []string,
	logger *slog.Logger,
	operations *jobqueue.Operations,
) error {
	flags := flag.NewFlagSet("retry-job", flag.ContinueOnError)
	jobID := flags.Int64("id", 0, "discarded or cancelled River job ID")
	actorID := flags.String("actor-id", "", "owner identifier recorded in the audit event")
	requestID := flags.String("request-id", "", "optional request or incident identifier")
	reason := flags.String("reason", "", "required retry reason recorded in the audit event")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	retried, err := operations.Retry(ctx, jobqueue.RetryRequest{
		JobID:     *jobID,
		ActorType: "owner",
		ActorID:   *actorID,
		RequestID: *requestID,
		Reason:    *reason,
	})
	if err != nil {
		return err
	}
	logger.InfoContext(
		ctx,
		"River job manually retried",
		"job_id", retried.ID,
		"kind", retried.Kind,
		"queue", retried.Queue,
		"state", retried.State,
	)
	return nil
}
