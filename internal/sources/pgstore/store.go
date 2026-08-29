package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/traweezy/relantern/internal/sources"
)

type Store struct{}

type Executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (Store) Sync(ctx context.Context, transaction Executor, registry sources.Registry) error {
	if _, err := transaction.Exec(ctx, `
		update app.sources
		set validation_state = 'paused', enabled = false, updated_at = now()
		where origin = 'system'`); err != nil {
		return fmt.Errorf("pause existing system sources: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.source_endpoints
		set next_poll_at = null, health_state = 'paused', updated_at = now()
		where source_id in (select id from app.sources where origin = 'system')`); err != nil {
		return fmt.Errorf("pause existing system endpoints: %w", err)
	}

	state := "paused"
	endpointState := "paused"
	if registry.Enabled {
		state = "active"
		endpointState = "unverified"
	}

	for _, source := range registry.Sources {
		if err := upsertSource(
			ctx,
			transaction,
			source.ID,
			source.Name,
			source.TrustTier,
			source.Owner,
			source.Origin,
			state,
			source.URL,
			source.ContentLicense,
			source.Enabled,
			source.Topics,
			source.ReviewedAt.Time,
		); err != nil {
			return err
		}
	}

	for _, repository := range registry.Repositories {
		homepageURL := "https://github.com/" + repository.RepositoryOwner + "/" + repository.RepositoryName
		if err := upsertSource(
			ctx,
			transaction,
			repository.ID,
			repository.Name,
			repository.TrustTier,
			repository.Owner,
			repository.Origin,
			state,
			homepageURL,
			repository.ContentLicense,
			repository.Enabled,
			repository.Topics,
			repository.ReviewedAt.Time,
		); err != nil {
			return err
		}
		if err := upsertRepository(ctx, transaction, repository); err != nil {
			return err
		}
	}

	for _, endpoint := range registry.Endpoints() {
		if err := upsertEndpoint(ctx, transaction, endpoint, endpointState); err != nil {
			return err
		}
	}
	return nil
}

func upsertSource(
	ctx context.Context,
	transaction Executor,
	id string,
	name string,
	trustTier sources.TrustTier,
	owner string,
	origin string,
	validationState string,
	homepageURL string,
	contentPolicy string,
	enabled bool,
	topics []string,
	reviewedAt time.Time,
) error {
	_, err := transaction.Exec(ctx, `
		insert into app.sources (
			id, name, trust_tier, owner, origin, validation_state,
			homepage_url, content_policy, enabled, topics, reviewed_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		on conflict (id) do update set
			name = excluded.name,
			trust_tier = excluded.trust_tier,
			owner = excluded.owner,
			origin = excluded.origin,
			validation_state = excluded.validation_state,
			homepage_url = excluded.homepage_url,
			content_policy = excluded.content_policy,
			enabled = excluded.enabled,
			topics = excluded.topics,
			reviewed_at = excluded.reviewed_at,
			updated_at = now()`,
		id,
		name,
		string(trustTier),
		owner,
		origin,
		validationState,
		homepageURL,
		contentPolicy,
		enabled,
		topics,
		reviewedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert source %q: %w", id, err)
	}
	return nil
}

func upsertRepository(ctx context.Context, transaction Executor, repository sources.Repository) error {
	events := make([]string, len(repository.EnabledEvents))
	for index, event := range repository.EnabledEvents {
		events[index] = string(event)
	}
	_, err := transaction.Exec(ctx, `
		insert into app.github_repositories (
			source_id, node_id, repository_owner, repository_name, enabled_events
		) values ($1, $2, $3, $4, $5)
		on conflict (source_id) do update set
			node_id = excluded.node_id,
			repository_owner = excluded.repository_owner,
			repository_name = excluded.repository_name,
			enabled_events = excluded.enabled_events,
			updated_at = now()`,
		repository.ID,
		repository.NodeID,
		repository.RepositoryOwner,
		repository.RepositoryName,
		events,
	)
	if err != nil {
		return fmt.Errorf("upsert repository %q: %w", repository.ID, err)
	}
	return nil
}

func upsertEndpoint(ctx context.Context, transaction Executor, endpoint sources.Endpoint, healthState string) error {
	configuration, err := endpointConfiguration(endpoint)
	if err != nil {
		return err
	}
	_, err = transaction.Exec(ctx, `
		insert into app.source_endpoints (
			registry_id, source_id, connector, url, poll_interval,
			priority, robots_policy, expected_content_types,
			max_response_bytes, fixture_suite, config, health_state
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		on conflict (registry_id) do update set
			source_id = excluded.source_id,
			connector = excluded.connector,
			url = excluded.url,
			poll_interval = excluded.poll_interval,
			priority = excluded.priority,
			robots_policy = excluded.robots_policy,
			expected_content_types = excluded.expected_content_types,
			max_response_bytes = excluded.max_response_bytes,
			fixture_suite = excluded.fixture_suite,
			config = excluded.config,
			next_poll_at = null,
			health_state = excluded.health_state,
			updated_at = now()`,
		endpoint.ID,
		endpoint.SourceID,
		string(endpoint.Connector),
		endpoint.URL,
		endpoint.PollInterval,
		string(endpoint.Priority),
		endpoint.RobotsPolicy,
		endpoint.ExpectedContentTypes,
		endpoint.MaxResponseBytes,
		endpoint.FixtureSuite,
		configuration,
		healthState,
	)
	if err != nil {
		return fmt.Errorf("upsert endpoint %q: %w", endpoint.ID, err)
	}
	return nil
}

func endpointConfiguration(endpoint sources.Endpoint) ([]byte, error) {
	if endpoint.RepositoryNodeID == "" {
		return []byte(`{}`), nil
	}
	configuration := struct {
		NodeID          string                  `json:"nodeId"`
		RepositoryOwner string                  `json:"repositoryOwner"`
		RepositoryName  string                  `json:"repositoryName"`
		Event           sources.RepositoryEvent `json:"event"`
	}{
		NodeID:          endpoint.RepositoryNodeID,
		RepositoryOwner: endpoint.RepositoryOwner,
		RepositoryName:  endpoint.RepositoryName,
		Event:           endpoint.RepositoryEvent,
	}
	encoded, err := json.Marshal(configuration)
	if err != nil {
		return nil, fmt.Errorf("encode endpoint %q configuration: %w", endpoint.ID, err)
	}
	return encoded, nil
}
