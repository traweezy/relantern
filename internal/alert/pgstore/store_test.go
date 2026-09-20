package pgstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/alert"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

func TestOfficialAdvisoryEndpoint(t *testing.T) {
	for _, test := range []struct {
		url  string
		want bool
	}{
		{"https://api.github.com/repos/acme/widget/security-advisories", true},
		{sources.GlobalReviewedAdvisoriesURL, true},
		{"https://api.github.com/advisories", false},
		{"https://api.github.com/advisories?type=reviewed&per_page=100", false},
		{"https://api.github.com/advisories?direction=desc&per_page=100&sort=updated&type=unreviewed", false},
		{"https://api.github.com/repos/acme/widget/releases", false},
		{"https://api.github.com.evil.test/repos/acme/widget/security-advisories", false},
		{"https://api.github.com:443/repos/acme/widget/security-advisories", false},
		{"https://api.github.com/repos/acme/widget/security-advisories?redirect=1", false},
		{"https://api.github.com/repos/acme/%2f/security-advisories", false},
		{"http://api.github.com/advisories", false},
	} {
		if got := officialAdvisoryEndpoint(test.url); got != test.want {
			t.Errorf("officialAdvisoryEndpoint(%q) = %t, want %t", test.url, got, test.want)
		}
	}
}

func TestOfficialAdvisoryURL(t *testing.T) {
	repository := "https://api.github.com/repos/acme/widget/security-advisories"
	global := sources.GlobalReviewedAdvisoriesURL
	id := "GHSA-abcd-1234-efgh"
	for _, test := range []struct {
		endpoint string
		url      string
		want     bool
	}{
		{repository, "https://github.com/acme/widget/security/advisories/" + id, true},
		{global, "https://github.com/advisories/" + id, true},
		{repository, "https://github.com/other/widget/security/advisories/" + id, false},
		{repository, "https://github.com/acme/widget/security/advisories/GHSA-xxxx-yyyy-zzzz", false},
		{global, "https://github.com/advisories/" + id + "?redirect=1", false},
		{global, "https://github.com.evil.test/advisories/" + id, false},
		{repository, repository + "?relantern_entry=deadbeef", false},
	} {
		if got := officialAdvisoryURL(test.endpoint, test.url, id); got != test.want {
			t.Errorf("officialAdvisoryURL(%q, %q) = %t, want %t",
				test.endpoint, test.url, got, test.want)
		}
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	if _, err := New(nil, nil, nil, nil); err == nil {
		t.Fatal("New() accepted missing dependencies")
	}
}

func TestReadAdvisoryUsesOfficialPublicLinkFromGlobalEntry(t *testing.T) {
	ctx := context.Background()
	endpoint := sources.GlobalReviewedAdvisoriesURL
	publicURL := "https://github.com/advisories/GHSA-abcd-1234-efgh"
	body := `[{"ghsa_id":"GHSA-abcd-1234-efgh","summary":"Critical widget update",` +
		`"html_url":"` + publicURL + `","type":"reviewed","severity":"critical",` +
		`"published_at":"2026-09-20T22:00:00Z","github_reviewed_at":"2026-09-20T22:01:00Z",` +
		`"vulnerabilities":[{"package":{"ecosystem":"go","name":"example.com/widget"},` +
		`"vulnerable_version_range":"< 1.2.3"}]}]`
	entries, err := parsing.SplitEntries(ctx, sources.ConnectorGitHubAdvisories, endpoint, []byte(body))
	if err != nil || len(entries) != 1 {
		t.Fatalf("SplitEntries() = %+v, %v", entries, err)
	}
	entry := entries[0]
	if entry.URL == publicURL {
		t.Fatal("global advisory fixture did not exercise the synthetic entry URL")
	}
	observedAt := time.Date(2026, 9, 20, 22, 2, 0, 0, time.UTC)
	identityHash := sha256.Sum256([]byte(entry.ExternalID))
	digest := sha256.Sum256(entry.Payload)
	objectKey, err := storage.RawObjectKey("test-alert-entry-"+hex.EncodeToString(identityHash[:]),
		observedAt, digest, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	store := Store{objects: &fixtureReader{payload: entry.Payload}}
	advisory, sourceURL, err := store.readAdvisory(ctx, evidence{
		objectKey: objectKey, rawSHA256: digest[:], sourceID: "test-alert",
		canonicalURL: entry.URL, entryID: entry.ExternalID,
		entryFirstSeenAt: observedAt, endpointURL: endpoint,
	})
	if err != nil || advisory.ID != "GHSA-abcd-1234-efgh" || sourceURL != publicURL {
		t.Fatalf("readAdvisory() = %+v, %q, %v", advisory, sourceURL, err)
	}
}

func TestGlobalAdvisoryPageRequiresMatchingFetchProvenance(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool, true)
	pageTwo := sources.GlobalReviewedAdvisoriesURL + "&after=cursor-2"
	fixture.exec(t, ctx, `update app.raw_documents set canonical_url = $2 where id = $1::uuid`,
		fixture.parentID, pageTwo)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: fixture.payload}, clock.NewFixed(fixture.now))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)

	wrongRegistryID := fixture.registryID + "-wrong"
	fixture.exec(t, ctx, `
		insert into app.source_endpoints (registry_id, source_id, connector, url,
			poll_interval, priority, robots_policy, expected_content_types,
			max_response_bytes, fixture_suite)
		values ($1, $2, 'github_advisories', $3, interval '1 hour', 'critical',
			'api', array['application/json'], 1048576, 'test')`,
		wrongRegistryID, fixture.sourceID,
		"https://api.github.com/repos/acme/widget/security-advisories")
	recordGlobalPageFetch(t, ctx, fixture, wrongRegistryID, pageTwo)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)

	recordGlobalPageFetch(t, ctx, fixture, fixture.registryID, pageTwo)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("assess fetched page-two advisory: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `
			delete from river.river_job where queue = 'test_alert_assessment'
				and kind = 'assess_critical_advisory' and args->>'rawDocumentId' = $1`,
			fixture.childID); err != nil {
			t.Errorf("clean up advisory catch-up job: %v", err)
		}
	})
	if err := store.CatchUp(ctx, jobqueue.ReassessCurrentAdvisoriesArgs{
		UserID: fixture.userID, SettingsVersion: 1,
	}); err != nil {
		t.Fatalf("catch up fetched page-two advisory: %v", err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 1)
}

func TestGlobalAdvisoryPageRejectsSpoofedURLWithFetchRecord(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool, true)
	spoofed := "https://api.github.com/advisories?direction=desc&per_page=100&sort=updated&type=unreviewed&after=cursor-2"
	fixture.exec(t, ctx, `update app.raw_documents set canonical_url = $2 where id = $1::uuid`,
		fixture.parentID, spoofed)
	recordGlobalPageFetch(t, ctx, fixture, fixture.registryID, spoofed)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: fixture.payload}, clock.NewFixed(fixture.now))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	if err := store.CatchUp(ctx, jobqueue.ReassessCurrentAdvisoriesArgs{
		UserID: fixture.userID, SettingsVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 0)
}

func recordGlobalPageFetch(t *testing.T, ctx context.Context, fixture assessmentFixture, registryID, pageURL string) {
	t.Helper()
	fixture.exec(t, ctx, `
		insert into app.source_fetches (endpoint_id, attempted_at, completed_at,
			outcome, status_code, final_url, content_type, bytes, duration_ms,
			raw_sha256, object_key)
		select endpoint.id, parent.first_seen_at, parent.first_fetched_at,
			'stored', 200, $3, 'application/json', 1, 1,
			parent.raw_sha256, parent.object_key
		from app.source_endpoints endpoint
		join app.raw_documents parent on parent.id = $2::uuid
		where endpoint.registry_id = $1`, registryID, fixture.parentID, pageURL)
}

type fixtureReader struct {
	payload []byte
}

func (reader *fixtureReader) Read(_ context.Context, _ string, _ int64) ([]byte, error) {
	return bytes.Clone(reader.payload), nil
}

func TestAssessCriticalAdvisoryTransactionAndEvidence(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	reader := &fixtureReader{payload: bytes.Clone(fixture.payload)}
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, reader, clock.NewFixed(fixture.now.Add(2*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}

	reader.payload[0] ^= 1
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err == nil {
		t.Fatal("Assess() accepted a child object with a mismatched SHA-256")
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	reader.payload = bytes.Clone(fixture.payload)

	fixture.exec(t, ctx, `update app.sources set enabled = false where id = $1`, fixture.sourceID)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("Assess() disabled source error = %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	fixture.exec(t, ctx, `update app.sources set enabled = true where id = $1`, fixture.sourceID)

	fixture.exec(t, ctx, `update app.items set status = 'suppressed' where id = $1::uuid`, fixture.itemID)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("Assess() stale item error = %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	fixture.exec(t, ctx, `update app.items set status = 'active' where id = $1::uuid`, fixture.itemID)

	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = null where id = $1::uuid`, fixture.watchID)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("Assess() untyped watch error = %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = 'go' where id = $1::uuid`, fixture.watchID)

	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("Assess() valid evidence error = %v", err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("Assess() replay error = %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	var alertID, advisoryID, ecosystem, packageName, currentVersion, versionRange, patchedVersion, title, sourceURL string
	var observedAt time.Time
	err = pool.QueryRow(ctx, `
		select id::text, advisory_id, ecosystem, package_name, current_version,
			vulnerable_range, patched_version, title, source_url, observed_at
		from app.critical_alerts where user_id = $1::uuid`, fixture.userID).Scan(
		&alertID, &advisoryID, &ecosystem, &packageName, &currentVersion,
		&versionRange, &patchedVersion, &title, &sourceURL, &observedAt)
	if err != nil {
		t.Fatal(err)
	}
	if advisoryID != "GHSA-abcd-1234-efgh" || ecosystem != "go" ||
		packageName != "example.com/widget" || currentVersion != "1.2.0" ||
		versionRange != "< 1.2.3" || patchedVersion != "1.2.3" ||
		title != "Critical widget update" || sourceURL != fixture.publicURL {
		t.Fatalf("immutable alert snapshot changed: %s %s %s %s %s %s %s %s",
			advisoryID, ecosystem, packageName, currentVersion, versionRange,
			patchedVersion, title, sourceURL)
	}
	if !observedAt.Equal(fixture.now) {
		t.Fatalf("alert observed_at = %s, want raw first_seen_at %s", observedAt, fixture.now)
	}
	rows, err := pool.Query(ctx, `
		select id::text, channel, idempotency_key, payload_sha256, next_attempt_at
		from app.critical_alert_deliveries where alert_id = $1::uuid order by channel`, alertID)
	if err != nil {
		t.Fatal(err)
	}
	var deliveryIDs []string
	wantScheduledAt := time.Date(2026, 9, 21, 7, 0, 0, 0, time.UTC)
	for rows.Next() {
		var id, channel, key string
		var hash []byte
		var nextAttemptAt time.Time
		if err := rows.Scan(&id, &channel, &key, &hash, &nextAttemptAt); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if !nextAttemptAt.Equal(wantScheduledAt) {
			t.Errorf("%s next_attempt_at = %s, want %s", channel, nextAttemptAt, wantScheduledAt)
		}
		expected := alert.DeliverySHA256(alert.Delivery{
			AlertID: alertID, Channel: channel, IdempotencyKey: key,
			Title: title, SourceURL: sourceURL, PackageName: packageName,
			Ecosystem: ecosystem, CurrentVersion: currentVersion,
			VersionRange: versionRange, PatchedVersion: patchedVersion,
		})
		if !bytes.Equal(hash, expected[:]) {
			t.Errorf("%s payload hash does not match immutable alert", channel)
		}
		deliveryIDs = append(deliveryIDs, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(deliveryIDs) != 2 {
		t.Fatalf("delivery intents = %d, want Discord and email", len(deliveryIDs))
	}
	for _, deliveryID := range deliveryIDs {
		var scheduledAt time.Time
		err := pool.QueryRow(ctx, `
			select scheduled_at from river.river_job
			where kind = 'deliver_critical_alert' and queue = 'test_alert_assessment'
				and args->>'deliveryId' = $1`, deliveryID).Scan(&scheduledAt)
		if err != nil || !scheduledAt.Equal(wantScheduledAt) {
			t.Errorf("delivery %s River schedule = %s, %v", deliveryID, scheduledAt, err)
		}
	}

	fixture.exec(t, ctx, `update app.watched_technologies set current_version = '2.0.0' where id = $1::uuid`, fixture.watchID)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("Assess() after watch edit error = %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	var retainedVersion string
	if err := pool.QueryRow(ctx, `select current_version from app.critical_alerts where id = $1::uuid`, alertID).Scan(&retainedVersion); err != nil || retainedVersion != "1.2.0" {
		t.Fatalf("immutable current version = %q, %v", retainedVersion, err)
	}
}

func TestAssessSupportingRevisionSkipsSupersededEntry(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: fixture.payload}, clock.NewFixed(fixture.now.Add(2*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}

	// A reviewed advisory may remain a supporting source after a different
	// story revision wins dedupe. Its own latest child is still valid evidence.
	primaryDigest := sha256.Sum256([]byte("another primary story"))
	var primaryRevisionID string
	err = pool.QueryRow(ctx, `
		insert into app.content_revisions (raw_document_id, normalized_sha256,
			normalized_text_object_key, parser_name, parser_version, title,
			language, normalized_bytes, outline, offset_map, warnings,
			change_kind, change_reason, material_change, observed_at)
		values ($1::uuid, $2, $3, 'github_advisories', '1', 'Other primary story',
			'en', 21, '[]', '[]', '[]', 'initial', 'initial parse', false, $4)
		returning id::text`, fixture.parentID, primaryDigest[:],
		"normalized/"+fixture.sourceID+"/"+hex.EncodeToString(primaryDigest[:])+".txt",
		fixture.now.Add(time.Minute)).Scan(&primaryRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.exec(t, ctx, `
		update app.item_sources set source_role = 'supporting', sort_order = 1
		where revision_id = $1::uuid`, fixture.revisionID)
	fixture.exec(t, ctx, `
		insert into app.item_sources (revision_id, item_id, canonical_url,
			source_role, source_tier, sort_order)
		values ($1::uuid, $2::uuid, $3, 'primary', 'T1', 0)`,
		primaryRevisionID, fixture.itemID, fixture.endpointURL)
	fixture.exec(t, ctx, `
		update app.items set current_revision_id = $2::uuid, status = 'updated'
		where id = $1::uuid`, fixture.itemID, primaryRevisionID)

	// A newer raw for the same source entry makes the prior advisory stale,
	// including while the newer body is awaiting parsing.
	newerBody := []byte(`{"advisory":"newer"}`)
	newerDigest := sha256.Sum256(newerBody)
	identityHash := sha256.Sum256([]byte(fixture.externalID))
	newerKey, err := storage.RawObjectKey(fixture.sourceID+"-entry-"+hex.EncodeToString(identityHash[:]),
		fixture.now, newerDigest, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	var newerID string
	err = pool.QueryRow(ctx, `
		insert into app.raw_documents (source_id, canonical_url, object_key,
			raw_sha256, first_seen_at, first_fetched_at, content_policy,
			parent_raw_document_id, source_entry_id, source_registry_id,
			source_connector, source_content_type,
			ingestion_error_code, ingestion_failed_at)
		values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt', $6::uuid,
			$7::uuid, $8, 'source_entry', 'application/json', 'pending_parse', $5)
		returning id::text`, fixture.sourceID, fixture.publicURL, newerKey, newerDigest[:],
		fixture.now.Add(time.Minute), fixture.parentID, fixture.entryID,
		fixture.registryID).Scan(&newerID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("Assess() superseded supporting child error = %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	fixture.exec(t, ctx, `delete from app.raw_documents where id = $1::uuid`, newerID)

	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("Assess() current supporting child error = %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	var admittedRevision, admittedItem, admittedURL string
	if err := pool.QueryRow(ctx, `
		select revision_id::text, item_id::text, source_url
		from app.critical_alerts where user_id = $1::uuid`, fixture.userID).Scan(
		&admittedRevision, &admittedItem, &admittedURL); err != nil {
		t.Fatal(err)
	}
	if admittedRevision != fixture.revisionID || admittedItem != fixture.itemID ||
		admittedURL != fixture.publicURL {
		t.Fatalf("supporting alert evidence = revision %s item %s URL %s",
			admittedRevision, admittedItem, admittedURL)
	}
}

type assessmentFixture struct {
	pool        *pgxpool.Pool
	now         time.Time
	sourceID    string
	userID      string
	watchID     string
	itemID      string
	childID     string
	revisionID  string
	parentID    string
	entryID     string
	registryID  string
	endpointURL string
	externalID  string
	publicURL   string
	payload     []byte
}

func seedAssessment(t *testing.T, ctx context.Context, pool *pgxpool.Pool, global ...bool) assessmentFixture {
	t.Helper()
	now := time.Date(2026, 9, 20, 23, 30, 0, 0, time.UTC)
	suffix := uuid.NewString()
	sourceID := "test-alert-" + suffix
	registryID := sourceID + "-endpoint"
	endpointURL := "https://api.github.com/repos/acme/widget/security-advisories"
	publicURL := "https://github.com/acme/widget/security/advisories/GHSA-abcd-1234-efgh"
	classification := `"state":"published",`
	if len(global) > 0 && global[0] {
		endpointURL = sources.GlobalReviewedAdvisoriesURL
		publicURL = "https://github.com/advisories/GHSA-abcd-1234-efgh"
		classification = `"type":"reviewed","github_reviewed_at":"2026-09-20T22:01:00Z",`
	}
	advisoryJSON := `[{"ghsa_id":"GHSA-abcd-1234-efgh","summary":"Critical widget update",` +
		`"html_url":"` + publicURL + `",` +
		`"severity":"critical",` + classification + `"published_at":"2026-09-20T22:00:00Z",` +
		`"vulnerabilities":[{"package":{"ecosystem":"go","name":"example.com/widget"},` +
		`"vulnerable_version_range":"< 1.2.3","patched_versions":"1.2.3"}]}]`
	entries, err := parsing.SplitEntries(ctx, sources.ConnectorGitHubAdvisories,
		endpointURL, []byte(advisoryJSON))
	if err != nil || len(entries) != 1 {
		t.Fatalf("SplitEntries() = %+v, %v", entries, err)
	}
	entry := entries[0]
	parentDigest := sha256.Sum256([]byte(advisoryJSON))
	parentKey, err := storage.RawObjectKey(sourceID, now, parentDigest, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	entryDigest := sha256.Sum256(entry.Payload)
	identityHash := sha256.Sum256([]byte(entry.ExternalID))
	childKey, err := storage.RawObjectKey(sourceID+"-entry-"+hex.EncodeToString(identityHash[:]),
		now, entryDigest, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	fixture := assessmentFixture{pool: pool, now: now, sourceID: sourceID,
		publicURL: publicURL, payload: entry.Payload, registryID: registryID,
		endpointURL: endpointURL, externalID: entry.ExternalID}
	t.Cleanup(func() { fixture.cleanup(t) })
	fixture.exec(t, ctx, `
		insert into app.sources (id, name, trust_tier, owner, origin,
			validation_state, homepage_url, content_policy, enabled, topics, reviewed_at)
		values ($1, 'Test advisory source', 'T1', 'Test owner', 'system',
			'active', 'https://github.com/acme/widget', 'link-and-excerpt', true,
			array['security'], $2)`, sourceID, now)
	fixture.exec(t, ctx, `
		insert into app.source_endpoints (registry_id, source_id, connector, url,
			poll_interval, priority, robots_policy, expected_content_types,
			max_response_bytes, fixture_suite)
		values ($1, $2, 'github_advisories', $3, interval '1 hour', 'critical',
			'api', array['application/json'], 1048576, 'test')`, registryID, sourceID, endpointURL)
	if err := pool.QueryRow(ctx, `
		insert into app.users (github_user_id, login, display_name, email, timezone)
		values ($1, $2, 'Test alert owner', $3, 'UTC') returning id::text`,
		time.Now().UnixNano(), "test-alert-"+suffix,
		"test-alert-"+suffix+"@example.test").Scan(&fixture.userID); err != nil {
		t.Fatal(err)
	}
	fixture.exec(t, ctx, `
		insert into app.owner_settings (user_id, quiet_hours_start, quiet_hours_end,
			critical_alerts_bypass, critical_alert_channels)
		values ($1::uuid, '22:00', '07:00', false, array['dashboard','discord','email'])`, fixture.userID)
	if err := pool.QueryRow(ctx, `
		insert into app.watched_technologies (user_id, technology, package_name,
			current_version, ecosystem, status)
		values ($1::uuid, 'Go widget', 'example.com/widget', '1.2.0', 'go', 'active')
		returning id::text`, fixture.userID).Scan(&fixture.watchID); err != nil {
		t.Fatal(err)
	}
	var parentID, entryID string
	if err := pool.QueryRow(ctx, `
		insert into app.raw_documents (source_id, canonical_url, object_key,
			raw_sha256, first_seen_at, first_fetched_at, content_policy,
			source_registry_id, source_connector, source_content_type)
		values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt', $6,
			'github_advisories', 'application/json') returning id::text`,
		sourceID, endpointURL, parentKey, parentDigest[:], now, registryID).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.source_entries (source_id, external_id, first_seen_at, last_seen_at)
		values ($1, $2, $3, $3) returning id::text`, sourceID, entry.ExternalID, now).Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	fixture.parentID = parentID
	fixture.entryID = entryID
	if err := pool.QueryRow(ctx, `
		insert into app.raw_documents (source_id, canonical_url, object_key,
			raw_sha256, first_seen_at, first_fetched_at, content_policy,
			parent_raw_document_id, source_entry_id, source_registry_id,
			source_connector, source_content_type)
		values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt', $6::uuid,
			$7::uuid, $8, 'source_entry', 'application/json') returning id::text`,
		sourceID, entry.URL, childKey, entryDigest[:], now, parentID,
		entryID, registryID).Scan(&fixture.childID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.content_revisions (raw_document_id, normalized_sha256,
			normalized_text_object_key, parser_name, parser_version, title,
			language, normalized_bytes, outline, offset_map, warnings,
			change_kind, change_reason, material_change, observed_at)
		values ($1::uuid, $2, $3, 'source_entry', '1', 'Critical widget update',
			'en', 23, '[]', '[]', '[]', 'initial', 'initial parse', false, $4)
		returning id::text`, fixture.childID, entryDigest[:],
		"normalized/"+sourceID+"/"+hex.EncodeToString(entryDigest[:])+".txt", now.Add(time.Minute)).Scan(&fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.items (current_revision_id, canonical_url, title,
			normalized_title, normalized_author, slug, lifecycle_state,
			first_seen_at, status, simhash)
		values ($1::uuid, $2, 'Critical widget update', 'critical widget update',
			'', $3, 'clustered', $4, 'active', $5) returning id::text`,
		fixture.revisionID, entry.URL, sourceID, now, make([]byte, 8)).Scan(&fixture.itemID); err != nil {
		t.Fatal(err)
	}
	fixture.exec(t, ctx, `
		insert into app.item_sources (revision_id, item_id, canonical_url,
			source_role, source_tier, sort_order)
		values ($1::uuid, $2::uuid, $3, 'primary', 'T1', 0)`,
		fixture.revisionID, fixture.itemID, entry.URL)
	return fixture
}

func (fixture assessmentFixture) exec(t *testing.T, ctx context.Context, query string, args ...any) {
	t.Helper()
	if _, err := fixture.pool.Exec(ctx, query, args...); err != nil {
		t.Fatalf("fixture query failed: %v", err)
	}
}

func (fixture assessmentFixture) cleanup(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	statements := []struct {
		query string
		arg   any
	}{
		{`delete from river.river_job where queue = 'test_alert_assessment' and kind = 'deliver_critical_alert'
			and args->>'deliveryId' in (
				select delivery.id::text from app.critical_alert_deliveries delivery
				join app.critical_alerts alert on alert.id = delivery.alert_id
				where alert.user_id = $1::uuid)`, fixture.userID},
		{`delete from app.critical_alert_deliveries where alert_id in
			(select id from app.critical_alerts where user_id = $1::uuid)`, fixture.userID},
		{`delete from app.critical_alerts where user_id = $1::uuid`, fixture.userID},
		{`delete from app.item_sources where item_id = $1::uuid`, fixture.itemID},
		{`delete from app.items where id = $1::uuid`, fixture.itemID},
		{`delete from app.content_revisions where raw_document_id in
			(select id from app.raw_documents where source_id = $1)`, fixture.sourceID},
		{`delete from app.raw_documents where source_id = $1`, fixture.sourceID},
		{`delete from app.source_entries where source_id = $1`, fixture.sourceID},
		{`delete from app.watched_technologies where user_id = $1::uuid`, fixture.userID},
		{`delete from app.owner_settings where user_id = $1::uuid`, fixture.userID},
		{`delete from app.users where id = $1::uuid`, fixture.userID},
		{`delete from app.source_endpoints where source_id = $1`, fixture.sourceID},
		{`delete from app.sources where id = $1`, fixture.sourceID},
	}
	for _, statement := range statements {
		if statement.arg == "" || statement.arg == nil {
			continue
		}
		if _, err := fixture.pool.Exec(ctx, statement.query, statement.arg); err != nil {
			t.Errorf("cleanup assessment fixture: %v", err)
		}
	}
}

func assertAlertCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from app.critical_alerts where user_id = $1::uuid`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("critical alert count = %d, want %d", count, want)
	}
}

func openAssessmentPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for advisory assessment integration test")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var version int
	if err := pool.QueryRow(context.Background(), `
		select version_id from public.goose_db_version
		where is_applied order by id desc limit 1`).Scan(&version); err != nil {
		t.Skipf("migrated PostgreSQL is required: %v", err)
	}
	if version < 27 {
		t.Skipf("critical alert schema migration 27 is required; database is at %d", version)
	}
	return pool
}
