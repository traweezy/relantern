package pgstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/intelligence"
)

func TestAlertHistoryCursorRejectsMalformedAndUnsupportedValues(t *testing.T) {
	t.Parallel()
	validID := uuid.NewString()
	validTime := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	encode := func(payload string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(payload))
	}
	for _, value := range []string{
		"not-base64!",
		strings.Repeat("x", maximumAlertCursorLength+1),
		encode(`{"v":2,"createdAt":"2026-09-20T12:00:00Z","id":"` + validID + `"}`),
		encode(`{"v":1,"createdAt":"2026-09-20T12:00:00+00:00","id":"` + validID + `"}`),
		encode(`{"v":1,"createdAt":"2026-09-20T12:00:00Z","id":"not-a-uuid"}`),
		encode(`{"v":1,"createdAt":"2026-09-20T12:00:00Z","id":"` + validID + `","extra":true}`),
		encode(`{"v":1,"createdAt":"2026-09-20T12:00:00Z","id":"` + validID + `"} true`),
	} {
		if _, _, err := decodeAlertCursor(value); !errors.Is(err, intelligence.ErrInvalidAlertHistory) {
			t.Errorf("decodeAlertCursor(%q) error = %v", value, err)
		}
	}
	cursor, err := encodeAlertCursor(validTime, validID)
	if err != nil {
		t.Fatal(err)
	}
	decoded, createdAt, err := decodeAlertCursor(cursor)
	if err != nil || decoded.ID != validID || !createdAt.Equal(validTime) {
		t.Fatalf("cursor round trip = %+v, %s, %v", decoded, createdAt, err)
	}
}

func TestAlertHistoryPaginatesOwnerAlertsAndDeliveryStates(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for alert history integration test")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	fixture := seedAlertHistory(t, ctx, pool)

	first, err := store.AlertHistory(ctx, fixture.ownerID, "", 20)
	if err != nil || len(first.Alerts) != 20 || first.NextCursor == nil {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	newAlertID := fixture.insertAlert(t, ctx, fixture.ownerID, 25, first.Alerts[0].AlertedAt.Add(time.Second))
	second, err := store.AlertHistory(ctx, fixture.ownerID, *first.NextCursor, 20)
	if err != nil || len(second.Alerts) != 5 || second.NextCursor != nil {
		t.Fatalf("second page = %+v, %v", second, err)
	}
	seen := make(map[string]bool, 25)
	var deliveredFound bool
	var correctionFound bool
	for _, item := range append(first.Alerts, second.Alerts...) {
		if seen[item.ID] || item.AlertedAt.After(time.Now().Add(-24*time.Hour)) {
			t.Fatalf("duplicated or newer-than-24h alert: %+v", item)
		}
		seen[item.ID] = true
		if item.ID == fixture.deliveryAlertID {
			if len(item.Deliveries) != 2 || item.Deliveries[0].Channel != "discord" ||
				item.Deliveries[0].State != "sent" || item.Deliveries[0].DeliveredAt == nil ||
				item.Deliveries[0].NextAttemptAt != nil || item.Deliveries[1].Channel != "email" ||
				item.Deliveries[1].State != "failed" || item.Deliveries[1].AttemptCount != 2 ||
				item.Deliveries[1].DeliveredAt != nil || item.Deliveries[1].NextAttemptAt == nil {
				t.Fatalf("delivery summaries = %+v", item.Deliveries)
			}
			payload, err := json.Marshal(item)
			if err != nil || strings.Contains(string(payload), "private-provider-id") ||
				strings.Contains(string(payload), "private-idempotency-key") {
				t.Fatalf("delivery payload exposed private fields: %s, %v", payload, err)
			}
			deliveredFound = true
		} else if item.ID == fixture.correctedAlertID {
			if item.CorrectionReason == nil || *item.CorrectionReason != "withdrawn" ||
				item.CorrectedAt == nil || item.CorrectedAt.Location() != time.UTC ||
				len(item.Deliveries) != 1 || item.Deliveries[0].State != "suppressed" ||
				item.Deliveries[0].NextAttemptAt != nil {
				t.Fatalf("corrected alert history = %+v", item)
			}
			correctionFound = true
		} else if len(item.Deliveries) != 0 {
			t.Fatalf("alert %s inherited another alert's deliveries", item.ID)
		} else if item.CorrectionReason != nil || item.CorrectedAt != nil {
			t.Fatalf("unmodified alert gained a correction: %+v", item)
		}
	}
	if len(seen) != 25 || !deliveredFound || !correctionFound {
		t.Fatalf("history covered %d alerts, found delivery = %t, correction = %t", len(seen), deliveredFound, correctionFound)
	}
	if seen[newAlertID] {
		t.Fatal("newer alert was inserted into an older cursor page")
	}
	if first.Alerts[len(first.Alerts)-1].AlertedAt != second.Alerts[0].AlertedAt {
		t.Fatal("fixture did not exercise a timestamp tie across page boundary")
	}
	cursor, cursorTime, err := decodeAlertCursor(*first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	planRows, err := pool.Query(ctx, `explain (analyze, buffers)
		select id::text, advisory_id, title, ecosystem, package_name,
			current_version, vulnerable_range, patched_version, source_url,
			observed_at, created_at
		from app.critical_alerts
		where user_id = $1::uuid and (created_at, id) < ($2::timestamptz, $3::uuid)
		order by created_at desc, id desc limit $4`, fixture.ownerID, cursorTime, cursor.ID, 21)
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			planRows.Close()
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := planRows.Err(); err != nil {
		planRows.Close()
		t.Fatal(err)
	}
	planRows.Close()
	t.Logf("owner alert history query uses keyset index: %t",
		strings.Contains(strings.Join(plan, "\n"), "idx_critical_alerts_user_created_at"))
	other, err := store.AlertHistory(ctx, fixture.otherOwnerID, "", 20)
	if err != nil || len(other.Alerts) != 1 || other.Alerts[0].ID != fixture.otherAlertID {
		t.Fatalf("other owner page = %+v, %v", other, err)
	}
	if seen[other.Alerts[0].ID] {
		t.Fatal("an alert crossed the owner boundary")
	}
	empty, err := store.AlertHistory(ctx, fixture.emptyOwnerID, "", 20)
	if err != nil || empty.Alerts == nil || len(empty.Alerts) != 0 || empty.NextCursor != nil {
		t.Fatalf("empty owner page = %+v, %v", empty, err)
	}
	if _, err := store.AlertHistory(ctx, fixture.ownerID, "malformed", 20); !errors.Is(err, intelligence.ErrInvalidAlertHistory) {
		t.Fatalf("malformed cursor error = %v", err)
	}
	if _, err := store.AlertHistory(ctx, fixture.ownerID, "", 101); !errors.Is(err, intelligence.ErrInvalidAlertHistory) {
		t.Fatalf("oversized limit error = %v", err)
	}
	recentID := fixture.insertAlert(t, ctx, fixture.ownerID, 26, time.Now().UTC().Truncate(time.Microsecond))
	recent, count, err := store.recentCriticalAlerts(ctx, fixture.ownerID, time.Now().UTC())
	if err != nil || count != 1 || len(recent) != 1 || recent[0].ID != recentID {
		t.Fatalf("active Today alerts = %+v, count %d, error %v", recent, count, err)
	}
	if _, err := pool.Exec(ctx, `update app.critical_alerts
		set correction_reason = 'severity_downgraded', correction_revision_id = $2::uuid,
			corrected_at = $3
		where id = $1::uuid`, recentID, fixture.revisionID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	recent, count, err = store.recentCriticalAlerts(ctx, fixture.ownerID, time.Now().UTC())
	if err != nil || count != 0 || len(recent) != 0 {
		t.Fatalf("corrected alert remained on Today: %+v, count %d, error %v", recent, count, err)
	}
}

type alertHistoryFixture struct {
	pool             *pgxpool.Pool
	sourceID         string
	ownerID          string
	otherOwnerID     string
	emptyOwnerID     string
	deliveryAlertID  string
	correctedAlertID string
	otherAlertID     string
	itemID           string
	rawID            string
	revisionID       string
}

func seedAlertHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) alertHistoryFixture {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	sourceID := "test-alert-history-" + suffix
	fixture := alertHistoryFixture{pool: pool, sourceID: sourceID}
	t.Cleanup(func() { fixture.cleanup(t) })
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed alert history: %v", err)
		}
	}
	base := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Microsecond)
	exec(`insert into app.sources (id, name, trust_tier, owner, origin,
		validation_state, homepage_url, content_policy, enabled, topics, reviewed_at)
		values ($1, 'Alert history fixture', 'T1', 'Test owner', 'system', 'active',
		'https://github.com/advisories', 'link-and-excerpt', true, array['security'], $2)`, sourceID, base)
	for index, destination := range []*string{&fixture.ownerID, &fixture.otherOwnerID, &fixture.emptyOwnerID} {
		login := fmt.Sprintf("test-alert-history-%s-%d", suffix, index)
		if err := pool.QueryRow(ctx, `insert into app.users
			(github_user_id, login, display_name, email, timezone)
			values ($1, $2, 'Alert history owner', $3, 'UTC') returning id::text`,
			time.Now().UnixNano()+int64(index), login, login+"@example.test").Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	digest := make([]byte, 32)
	copy(digest, []byte(suffix))
	if err := pool.QueryRow(ctx, `insert into app.raw_documents
		(source_id, canonical_url, object_key, raw_sha256, first_seen_at, first_fetched_at, content_policy)
		values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt') returning id::text`,
		sourceID, "https://github.com/advisories/fixture-"+suffix,
		"test-alert-history/"+suffix+".json", digest, base).Scan(&fixture.rawID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into app.content_revisions
		(raw_document_id, normalized_sha256, normalized_text_object_key, parser_name,
		parser_version, title, language, normalized_bytes, outline, offset_map,
		warnings, change_kind, change_reason, material_change, observed_at)
		values ($1::uuid, $2, $3, 'test', '1', 'Alert history fixture', 'en',
		1, '[]', '[]', '[]', 'initial', 'fixture', false, $4) returning id::text`,
		fixture.rawID, digest, "test-alert-history/"+suffix+".txt", base).Scan(&fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into app.items
		(current_revision_id, canonical_url, title, normalized_title,
		normalized_author, slug, lifecycle_state, first_seen_at, status, simhash)
		values ($1::uuid, $2, 'Alert history fixture', 'alert history fixture',
		'', $3, 'clustered', $4, 'active', $5) returning id::text`,
		fixture.revisionID, "https://github.com/advisories/fixture-"+suffix,
		"test-alert-history-"+suffix, base, make([]byte, 8)).Scan(&fixture.itemID); err != nil {
		t.Fatal(err)
	}
	for number := range 25 {
		alertID := fixture.insertAlert(t, ctx, fixture.ownerID, number, base)
		if number == 0 {
			fixture.deliveryAlertID = alertID
		} else if number == 1 {
			fixture.correctedAlertID = alertID
		}
	}
	fixture.otherAlertID = fixture.insertAlert(t, ctx, fixture.otherOwnerID, 0, base)
	exec(`insert into app.critical_alert_deliveries
		(alert_id, channel, state, idempotency_key, payload_sha256,
		next_attempt_at, attempt_count, provider_id, delivered_at)
		values ($1::uuid, 'discord', 'sent', $2, $3, $4, 1,
		'private-provider-id', $4)`, fixture.deliveryAlertID,
		"private-idempotency-key-discord-"+suffix, digest, base.Add(time.Minute))
	exec(`insert into app.critical_alert_deliveries
		(alert_id, channel, state, idempotency_key, payload_sha256,
		next_attempt_at, attempt_count)
		values ($1::uuid, 'email', 'failed', $2, $3, $4, 2)`, fixture.deliveryAlertID,
		"private-idempotency-key-email-"+suffix, digest, base.Add(time.Hour))
	exec(`update app.critical_alerts
		set correction_reason = 'withdrawn', correction_revision_id = $2::uuid,
			corrected_at = $3
		where id = $1::uuid`, fixture.correctedAlertID, fixture.revisionID, base.Add(time.Minute))
	exec(`insert into app.critical_alert_deliveries
		(alert_id, channel, state, idempotency_key, payload_sha256, next_attempt_at)
		values ($1::uuid, 'discord', 'suppressed', $2, $3, $4)`, fixture.correctedAlertID,
		"private-idempotency-key-suppressed-"+suffix, digest, base.Add(time.Minute))
	return fixture
}

func (fixture alertHistoryFixture) insertAlert(t *testing.T, ctx context.Context, userID string, number int, createdAt time.Time) string {
	t.Helper()
	advisoryID := fmt.Sprintf("GHSA-%04x-%04x-%04x", number, 0, 0)
	var alertID string
	if err := fixture.pool.QueryRow(ctx, `insert into app.critical_alerts
		(user_id, advisory_id, ecosystem, package_name, current_version,
		vulnerable_range, raw_document_id, revision_id, item_id, source_id,
		source_url, title, observed_at, created_at)
		values ($1::uuid, $2, 'go', 'example.com/widget', '1.2.0', '< 1.2.3',
		$3::uuid, $4::uuid, $5::uuid, $6, $7, 'Critical widget update', $8, $9)
		returning id::text`, userID, advisoryID, fixture.rawID, fixture.revisionID,
		fixture.itemID, fixture.sourceID, "https://github.com/advisories/"+advisoryID,
		createdAt, createdAt).Scan(&alertID); err != nil {
		t.Fatal(err)
	}
	return alertID
}

func (fixture alertHistoryFixture) cleanup(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := fixture.pool.Exec(ctx, query, args...); err != nil {
			t.Errorf("clean alert history fixture: %v", err)
		}
	}
	owners := make([]string, 0, 2)
	for _, userID := range []string{fixture.ownerID, fixture.otherOwnerID, fixture.emptyOwnerID} {
		if userID != "" {
			owners = append(owners, userID)
		}
	}
	if len(owners) > 0 {
		exec(`delete from app.critical_alerts where user_id = any($1::uuid[])`, owners)
		exec(`delete from app.users where id = any($1::uuid[])`, owners)
	}
	if fixture.itemID != "" {
		exec(`delete from app.items where id = $1::uuid`, fixture.itemID)
	}
	if fixture.revisionID != "" {
		exec(`delete from app.content_revisions where id = $1::uuid`, fixture.revisionID)
	}
	if fixture.rawID != "" {
		exec(`delete from app.raw_documents where id = $1::uuid`, fixture.rawID)
	}
	exec(`delete from app.sources where id = $1`, fixture.sourceID)
}
