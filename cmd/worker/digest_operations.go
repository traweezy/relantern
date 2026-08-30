package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/controlplane"
	controlplanestore "github.com/traweezy/relantern/internal/controlplane/pgstore"
	"github.com/traweezy/relantern/internal/database"
	digeststore "github.com/traweezy/relantern/internal/digest/pgstore"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func runDigestOperation(arguments []string) error {
	common, err := config.LoadCommon()
	if err != nil {
		return fmt.Errorf("load common configuration: %w", err)
	}
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
	jobs, err := jobqueue.NewInserter()
	if err != nil {
		return err
	}
	switch arguments[0] {
	case "digest-preview":
		flags := flag.NewFlagSet("digest-preview", flag.ContinueOnError)
		userIDFlag := flags.String("user-id", "", "owner UUID; inferred when exactly one owner exists")
		scheduleIDFlag := flags.String("schedule-id", "", "daily digest schedule UUID; inferred when unique")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		userID, scheduleID, scopeErr := digestOperationScope(ctx, pool, *userIDFlag, *scheduleIDFlag)
		if scopeErr != nil {
			return scopeErr
		}
		store, storeErr := digeststore.New(pool, jobs)
		if storeErr != nil {
			return storeErr
		}
		preview, previewErr := store.Preview(ctx, userID, scheduleID, common.Clock.Now())
		if previewErr != nil {
			return previewErr
		}
		return encodeDigestOperation(preview)
	case "digest-run":
		flags := flag.NewFlagSet("digest-run", flag.ContinueOnError)
		userIDFlag := flags.String("user-id", "", "owner UUID; inferred when exactly one owner exists")
		scheduleIDFlag := flags.String("schedule-id", "", "daily digest schedule UUID; inferred when unique")
		deliver := flags.Bool("deliver", false, "allow configured delivery channels after environment fuses pass")
		idempotencyKey := flags.String("idempotency-key", uuid.NewString(), "stable owner retry key")
		output := flags.String("output", "json", "output format: json or occurrence-id")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if *output != "json" && *output != "occurrence-id" {
			return errors.New("digest-run output must be json or occurrence-id")
		}
		userID, scheduleID, scopeErr := digestOperationScope(ctx, pool, *userIDFlag, *scheduleIDFlag)
		if scopeErr != nil {
			return scopeErr
		}
		if *deliver {
			deliveryConfig, loadErr := config.LoadDelivery(common.Environment)
			if loadErr != nil {
				return fmt.Errorf("validate delivery fuse: %w", loadErr)
			}
			if deliveryConfig.Mode == "disabled" {
				return errors.New("deliver=true requires an enabled local capture or reviewed live delivery mode")
			}
		}
		store, storeErr := controlplanestore.New(pool, jobs)
		if storeErr != nil {
			return storeErr
		}
		service, serviceErr := controlplane.NewService(store, controlplane.DeploymentMetadata{
			Environment: string(common.Environment), Version: common.Version, GitSHA: common.GitSHA,
		})
		if serviceErr != nil {
			return serviceErr
		}
		result, actionErr := service.ActOnSchedule(ctx, controlplane.ScheduleActionRequest{
			UserID: userID, ScheduleID: scheduleID, Action: "run_now",
			Reason:         "Owner requested a local digest run from make digest-run",
			IdempotencyKey: *idempotencyKey, Deliver: *deliver,
		}, common.Clock.Now())
		if actionErr != nil {
			return actionErr
		}
		if *output == "occurrence-id" {
			_, err := fmt.Fprintln(os.Stdout, result.OccurrenceID)
			return err
		}
		return encodeDigestOperation(result)
	default:
		return errors.New("unsupported digest operation")
	}
}

func digestOperationScope(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID string,
	scheduleID string,
) (string, string, error) {
	rows, err := pool.Query(ctx, `
		select schedule.user_id::text, schedule.id::text
		from app.schedule_definitions schedule
		join app.users owner on owner.id = schedule.user_id
		where schedule.schedule_type = 'daily_digest'
			and ($1 = '' or schedule.user_id::text = $1)
			and ($2 = '' or schedule.id::text = $2)
			and ($1 <> '' or $2 <> '' or $3 = '' or owner.github_user_id::text = $3)
		order by schedule.user_id, schedule.id
		limit 2`, userID, scheduleID, os.Getenv("AUTH_ALLOWED_GITHUB_USER_ID"))
	if err != nil {
		return "", "", fmt.Errorf("resolve daily digest schedule: %w", err)
	}
	defer rows.Close()
	type scope struct{ userID, scheduleID string }
	matches := make([]scope, 0, 2)
	for rows.Next() {
		var match scope
		if err := rows.Scan(&match.userID, &match.scheduleID); err != nil {
			return "", "", fmt.Errorf("scan daily digest schedule: %w", err)
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return "", "", fmt.Errorf("iterate daily digest schedules: %w", err)
	}
	if len(matches) == 0 {
		return "", "", errors.New("no matching daily digest schedule exists")
	}
	if len(matches) > 1 {
		return "", "", errors.New("multiple daily digest schedules match; pass --user-id or --schedule-id")
	}
	return matches[0].userID, matches[0].scheduleID, nil
}

func encodeDigestOperation(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode digest operation: %w", err)
	}
	return nil
}
