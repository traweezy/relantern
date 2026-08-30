package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/retention"
	retentionstore "github.com/traweezy/relantern/internal/retention/pgstore"
	"github.com/traweezy/relantern/internal/storage/s3store"
)

func runRetentionOperation(arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("usage: worker retention-run")
	}
	common, err := config.LoadCommon()
	if err != nil {
		return fmt.Errorf("load common configuration: %w", err)
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		return fmt.Errorf("load database configuration: %w", err)
	}
	objectStorageConfig, err := config.LoadObjectStorage(common.Environment)
	if err != nil {
		return fmt.Errorf("load object-storage configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, databaseConfig)
	if err != nil {
		return err
	}
	defer pool.Close()
	objects, err := s3store.New(s3store.Config{
		Endpoint: objectStorageConfig.Endpoint, Bucket: objectStorageConfig.Bucket,
		Region: objectStorageConfig.Region, AccessKey: objectStorageConfig.AccessKey,
		SecretKey: objectStorageConfig.SecretKey,
	})
	if err != nil {
		return fmt.Errorf("create retention object store: %w", err)
	}
	repository, err := retentionstore.New(pool)
	if err != nil {
		return err
	}
	runner, err := retention.NewRunner(repository, objects, retention.DefaultPolicy())
	if err != nil {
		return err
	}
	counts, err := runner.Run(ctx, common.Clock.Now().UTC())
	if errors.Is(err, retention.ErrBusy) {
		return errors.New("retention is already running")
	}
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(counts); err != nil {
		return fmt.Errorf("encode retention result: %w", err)
	}
	return nil
}
