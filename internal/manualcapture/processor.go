package manualcapture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/dedupe"
	"github.com/traweezy/relantern/internal/embedding"
	"github.com/traweezy/relantern/internal/fetcher"
	fetchstore "github.com/traweezy/relantern/internal/fetcher/pgstore"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

const (
	manualUserAgent = "Relantern/1.0 (+https://github.com/traweezy/relantern)"
	robotsLimit     = int64(256 << 10)
)

var (
	ErrInvalidCapture = errors.New("manual capture is invalid")
	ErrPermanent      = errors.New("manual capture failed permanently")
)

type Embedder interface {
	Embed(context.Context, string) (embedding.Vector, error)
}

type Deduper interface {
	Process(context.Context, dedupe.ProcessRequest) (dedupe.ProcessResult, error)
}

type Parser interface {
	Process(context.Context, parsing.ProcessRequest) (parsing.ProcessResult, error)
}

type ObjectStore interface {
	storage.RawStore
	storage.ObjectReader
}

type Processor struct {
	pool               *pgxpool.Pool
	clock              clock.Clock
	objects            ObjectStore
	embedder           Embedder
	deduper            Deduper
	parser             Parser
	fetchRecords       fetchstore.Store
	jobs               *jobqueue.Inserter
	limiter            fetcher.RequestLimiter
	modelID            string
	allowLocalFixtures bool
}

type capture struct {
	ID                   string
	State                string
	SourceID             string
	RegistryID           string
	URL                  string
	Connector            string
	ContentPolicy        string
	ExpectedContentTypes []string
	MaxResponseBytes     int64
}

func New(
	pool *pgxpool.Pool,
	configuredClock clock.Clock,
	objects ObjectStore,
	embedder Embedder,
	deduper Deduper,
	parser Parser,
	jobs *jobqueue.Inserter,
	limiter fetcher.RequestLimiter,
	modelID string,
	allowLocalFixtures bool,
) (*Processor, error) {
	if pool == nil || configuredClock == nil || objects == nil || embedder == nil || deduper == nil || parser == nil || jobs == nil || limiter == nil {
		return nil, errors.New("manual capture processor requires database, clock, storage, provider, pipeline, queue, and limiter dependencies")
	}
	if strings.TrimSpace(modelID) == "" {
		return nil, errors.New("manual capture processor requires an embedding model")
	}
	return &Processor{
		pool: pool, clock: configuredClock, objects: objects, embedder: embedder,
		deduper: deduper, parser: parser, jobs: jobs, limiter: limiter,
		modelID: modelID, allowLocalFixtures: allowLocalFixtures,
	}, nil
}

func (processor *Processor) Process(ctx context.Context, captureID string) error {
	if _, err := uuid.Parse(captureID); err != nil {
		return fmt.Errorf("%w: capture ID must be a UUID", ErrInvalidCapture)
	}
	selected, err := processor.load(ctx, captureID)
	if err != nil {
		return err
	}
	if selected.State == "completed" || selected.State == "failed" {
		return nil
	}
	if err := processor.setStage(ctx, selected.ID, "fetching"); err != nil {
		return err
	}
	endpointURL, err := url.Parse(selected.URL)
	if err != nil {
		return processor.fail(ctx, selected.ID, "invalid_url", fmt.Errorf("%w: stored URL is invalid", ErrPermanent))
	}
	fixtureTargets := []fetcher.FixtureTarget{}
	if endpointURL.Scheme == "http" {
		if !processor.allowLocalFixtures || endpointURL.Hostname() != "fake-source" || endpointURL.Port() != "8090" {
			return processor.fail(ctx, selected.ID, "destination_denied", fmt.Errorf("%w: local fixture URL is not allowed", ErrPermanent))
		}
		fixtureTargets = append(fixtureTargets, fetcher.FixtureTarget{Host: "fake-source", Port: 8090})
	}
	policy, err := fetcher.NewPolicy(net.DefaultResolver, []string{endpointURL.Hostname()}, fixtureTargets)
	if err != nil {
		return processor.fail(ctx, selected.ID, "invalid_url", fmt.Errorf("%w: %v", ErrPermanent, err))
	}
	client, err := fetcher.NewSecureHTTPClient(policy, nil, fetcher.DefaultNetworkLimits(), 3)
	if err != nil {
		return processor.fail(ctx, selected.ID, "transport_policy", fmt.Errorf("%w: %v", ErrPermanent, err))
	}
	if err := checkRobots(ctx, client, endpointURL, manualUserAgent); err != nil {
		return processor.fail(ctx, selected.ID, "robots_denied", fmt.Errorf("%w: %v", ErrPermanent, err))
	}
	configuredFetcher, err := fetcher.New(
		fetcher.DefaultConfig(manualUserAgent), policy, client, processor.limiter, processor.objects,
	)
	if err != nil {
		return processor.fail(ctx, selected.ID, "fetch_configuration", fmt.Errorf("%w: %v", ErrPermanent, err))
	}
	checkpoint, err := processor.checkpoint(ctx, selected.RegistryID)
	if err != nil {
		return err
	}
	result, fetchErr := configuredFetcher.Fetch(ctx, fetcher.Endpoint{
		ID: selected.RegistryID, SourceID: selected.SourceID, URL: selected.URL,
		AllowedHosts:         []string{endpointURL.Hostname()},
		ExpectedContentTypes: selected.ExpectedContentTypes,
		MaxBodyBytes:         selected.MaxResponseBytes, ContentPolicy: selected.ContentPolicy,
	}, checkpoint)
	if recordErr := processor.recordFetch(ctx, selected, result); recordErr != nil {
		if fetchErr != nil {
			return errors.Join(fetchErr, recordErr)
		}
		return recordErr
	}
	if fetchErr != nil {
		var typed *fetcher.FetchError
		if errors.As(fetchErr, &typed) && typed.Retryable {
			return fetchErr
		}
		code := "fetch_failed"
		if errors.As(fetchErr, &typed) {
			code = string(typed.Code)
		}
		return processor.fail(ctx, selected.ID, code, fmt.Errorf("%w: %v", ErrPermanent, fetchErr))
	}
	if result.Outcome != fetcher.OutcomeStored || result.ObjectKey == "" {
		return processor.fail(ctx, selected.ID, "content_policy_denied", fmt.Errorf("%w: capture did not retain parseable content", ErrPermanent))
	}
	rawDocumentID, err := processor.rawDocumentID(ctx, selected.SourceID, result)
	if err != nil {
		return err
	}
	payload, err := processor.objects.Read(ctx, result.ObjectKey, selected.MaxResponseBytes)
	if err != nil {
		return fmt.Errorf("read manual-capture raw object: %w", err)
	}
	if err := processor.setStage(ctx, selected.ID, "parsing"); err != nil {
		return err
	}
	finalAttempt := result.Attempts[len(result.Attempts)-1]
	parsed, err := processor.parser.Process(ctx, parsing.ProcessRequest{
		RawDocumentID: rawDocumentID,
		SourceID:      selected.SourceID,
		Parse: parsing.Request{
			Connector: sources.Connector(selected.Connector), URL: finalAttempt.FinalURL,
			ContentType: finalAttempt.ContentType, Body: bytes.NewReader(payload),
			MaxBytes: selected.MaxResponseBytes,
		},
	})
	if err != nil {
		return processor.fail(ctx, selected.ID, "parse_failed", fmt.Errorf("%w: %v", ErrPermanent, err))
	}
	if err := processor.setStage(ctx, selected.ID, "deduplicating"); err != nil {
		return err
	}
	embeddingInput, err := boundedEmbeddingInput(parsed.Parsed.NormalizedText)
	if err != nil {
		return processor.fail(ctx, selected.ID, "embedding_input", fmt.Errorf("%w: %v", ErrPermanent, err))
	}
	vector, err := processor.embedder.Embed(ctx, embeddingInput)
	if err != nil {
		return fmt.Errorf("embed manual capture: %w", err)
	}
	decision, err := processor.deduper.Process(ctx, dedupe.ProcessRequest{
		RevisionID: parsed.Recorded.RevisionID, NormalizedText: parsed.Parsed.NormalizedText,
		DeclaredCanonicalURL: parsed.Parsed.CanonicalURL,
		EmbeddingModelID:     processor.modelID, Embedding: vector,
	})
	if err != nil {
		return fmt.Errorf("deduplicate manual capture: %w", err)
	}
	return processor.complete(ctx, selected.ID, decision.ItemID, decision.ClusterID, parsed.Recorded.RevisionID)
}

func (processor *Processor) load(ctx context.Context, captureID string) (capture, error) {
	var selected capture
	err := processor.pool.QueryRow(ctx, `
		select capture.id::text, capture.state, source.id, endpoint.registry_id,
			endpoint.url, endpoint.connector, source.content_policy,
			endpoint.expected_content_types, endpoint.max_response_bytes
		from app.manual_captures capture
		join app.sources source on source.id = capture.source_id
		join app.source_endpoints endpoint on endpoint.id = capture.endpoint_id
		where capture.id = $1::uuid`, captureID).Scan(
		&selected.ID, &selected.State, &selected.SourceID, &selected.RegistryID,
		&selected.URL, &selected.Connector, &selected.ContentPolicy,
		&selected.ExpectedContentTypes, &selected.MaxResponseBytes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return capture{}, fmt.Errorf("%w: capture does not exist", ErrInvalidCapture)
	}
	if err != nil {
		return capture{}, fmt.Errorf("select manual capture: %w", err)
	}
	return selected, nil
}

func (processor *Processor) checkpoint(ctx context.Context, registryID string) (fetcher.Checkpoint, error) {
	var checkpoint fetcher.Checkpoint
	var cursor pgtype.Text
	var etag pgtype.Text
	var lastModified pgtype.Text
	var providerState []byte
	err := processor.pool.QueryRow(ctx, `
		select checkpoint.cursor, checkpoint.etag, checkpoint.last_modified, checkpoint.provider_state
		from app.source_checkpoints checkpoint
		join app.source_endpoints endpoint on endpoint.id = checkpoint.endpoint_id
		where endpoint.registry_id = $1`, registryID).Scan(&cursor, &etag, &lastModified, &providerState)
	if errors.Is(err, pgx.ErrNoRows) {
		return fetcher.Checkpoint{}, nil
	}
	if err != nil {
		return fetcher.Checkpoint{}, fmt.Errorf("select manual-capture checkpoint: %w", err)
	}
	checkpoint.Cursor = cursor.String
	checkpoint.ETag = etag.String
	checkpoint.LastModified = lastModified.String
	checkpoint.ProviderState = map[string]any{}
	return checkpoint, nil
}

func (processor *Processor) recordFetch(ctx context.Context, selected capture, result fetcher.Result) error {
	if len(result.Attempts) == 0 {
		return nil
	}
	tx, err := processor.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin manual-capture fetch record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := processor.fetchRecords.Record(ctx, tx, selected.RegistryID, selected.SourceID, selected.ContentPolicy, result); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit manual-capture fetch record: %w", err)
	}
	return nil
}

func (processor *Processor) rawDocumentID(ctx context.Context, sourceID string, result fetcher.Result) (string, error) {
	var id string
	err := processor.pool.QueryRow(ctx, `
		select id::text from app.raw_documents
		where source_id = $1 and raw_sha256 = $2 and object_key = $3
		order by first_fetched_at desc, id desc limit 1`, sourceID, result.SHA256[:], result.ObjectKey).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("select manual-capture raw document: %w", err)
	}
	return id, nil
}

func (processor *Processor) setStage(ctx context.Context, captureID string, state string) error {
	now := processor.clock.Now().UTC()
	result, err := processor.pool.Exec(ctx, `
		update app.manual_captures
		set state = $2, started_at = coalesce(started_at, $3),
			error_code = null, completed_at = null, updated_at = $3
		where id = $1::uuid and state not in ('completed', 'failed')`, captureID, state, now)
	if err != nil {
		return fmt.Errorf("set manual-capture stage: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("%w: capture is already terminal", ErrInvalidCapture)
	}
	return nil
}

func (processor *Processor) complete(
	ctx context.Context,
	captureID string,
	itemID string,
	clusterID string,
	revisionID string,
) error {
	tx, err := processor.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin manual-capture completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := processor.clock.Now().UTC()
	result, err := tx.Exec(ctx, `
		update app.manual_captures
		set state = 'completed', item_id = $2::uuid, cluster_id = $3::uuid,
			error_code = null, completed_at = $4, updated_at = $4
		where id = $1::uuid and state = 'deduplicating'`, captureID, itemID, clusterID, now)
	if err != nil {
		return fmt.Errorf("complete manual capture: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("%w: capture changed before completion", ErrInvalidCapture)
	}
	if _, _, err := processor.jobs.EnqueueReembedEntity(ctx, tx, jobqueue.ReembedEntityArgs{
		EntityType: "item", EntityID: itemID, RevisionID: revisionID, ModelID: processor.modelID,
	}); err != nil {
		return err
	}
	if _, _, err := processor.jobs.EnqueueExtractItem(ctx, tx, jobqueue.ExtractItemArgs{
		ItemID: itemID, RevisionID: revisionID,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit manual-capture completion: %w", err)
	}
	return nil
}

func (processor *Processor) fail(ctx context.Context, captureID string, code string, cause error) error {
	now := processor.clock.Now().UTC()
	if len(code) > 100 {
		code = code[:100]
	}
	_, err := processor.pool.Exec(ctx, `
		update app.manual_captures
		set state = 'failed', error_code = $2,
			started_at = coalesce(started_at, $3), completed_at = $3, updated_at = $3
		where id = $1::uuid and state <> 'completed'`, captureID, code, now)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("record manual-capture failure: %w", err))
	}
	return cause
}

func boundedEmbeddingInput(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || !utf8.ValidString(trimmed) {
		return "", errors.New("normalized content is empty or invalid UTF-8")
	}
	payload := []byte(trimmed)
	if len(payload) <= embedding.MaximumInputBytes {
		return trimmed, nil
	}
	payload = payload[:embedding.MaximumInputBytes]
	for !utf8.Valid(payload) {
		payload = payload[:len(payload)-1]
	}
	return string(payload), nil
}

func checkRobots(ctx context.Context, client *http.Client, target *url.URL, userAgent string) error {
	robotsURL := *target
	robotsURL.Path = "/robots.txt"
	robotsURL.RawPath = ""
	robotsURL.RawQuery = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create robots request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "text/plain")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request robots policy: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("robots policy returned HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, robotsLimit+1))
	if err != nil {
		return fmt.Errorf("read robots policy: %w", err)
	}
	if int64(len(payload)) > robotsLimit {
		return errors.New("robots policy exceeds 256 KiB")
	}
	if robotsDisallows(string(payload), target.EscapedPath()) {
		return errors.New("robots policy disallows this URL")
	}
	return nil
}

func robotsDisallows(document string, path string) bool {
	active := false
	for _, line := range strings.Split(document, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		name, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "user-agent":
			active = strings.TrimSpace(value) == "*"
		case "disallow":
			rule := strings.TrimSpace(value)
			if active && rule != "" && strings.HasPrefix(path, rule) {
				return true
			}
		}
	}
	return false
}
