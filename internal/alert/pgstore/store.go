package pgstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/alert"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

const maximumAdvisoryEntryBytes int64 = 1 << 20

// Store admits confirmed, affected advisories and their external delivery
// intents together. Delivery is performed later from the immutable ledger.
type Store struct {
	pool    *pgxpool.Pool
	jobs    *jobqueue.Inserter
	objects storage.ObjectReader
	clock   clock.Clock
}

func New(pool *pgxpool.Pool, jobs *jobqueue.Inserter, objects storage.ObjectReader, clock clock.Clock) (*Store, error) {
	if pool == nil || jobs == nil || objects == nil || clock == nil {
		return nil, errors.New("critical advisory store requires database, jobs, objects, and clock")
	}
	return &Store{pool: pool, jobs: jobs, objects: objects, clock: clock}, nil
}

type evidence struct {
	objectKey        string
	rawSHA256        []byte
	sourceID         string
	canonicalURL     string
	entryRowID       string
	entryID          string
	entryFirstSeenAt time.Time
	endpointURL      string
	parentURL        string
	title            string
	observedAt       time.Time
	itemID           string
	itemStatus       string
}

const evidenceQuery = `
	select child.object_key, child.raw_sha256, child.source_id,
		child.canonical_url, entry.id::text, entry.external_id, entry.first_seen_at,
		endpoint.url, parent.canonical_url, revision.title,
		child.first_seen_at, item.id::text, item.status
	from app.raw_documents child
	join app.raw_documents parent on parent.id = child.parent_raw_document_id
	join app.source_entries entry on entry.id = child.source_entry_id
	join app.sources source on source.id = child.source_id
	join app.source_endpoints endpoint on endpoint.registry_id = child.source_registry_id
	join app.content_revisions revision on revision.raw_document_id = child.id
	join app.item_sources item_source on item_source.revision_id = revision.id
	join app.items item on item.id = item_source.item_id
	where child.id = $1::uuid and revision.id = $2::uuid
		and child.source_id = parent.source_id and child.source_id = entry.source_id
		and parent.source_registry_id = child.source_registry_id
		and parent.source_connector = 'github_advisories'
		and child.source_connector = 'source_entry'
		and endpoint.source_id = child.source_id
		and endpoint.connector = 'github_advisories'
		and (parent.canonical_url = endpoint.url or (
			endpoint.url = '` + sources.GlobalReviewedAdvisoriesURL + `' and exists (
				select 1 from app.source_fetches source_fetch
				where source_fetch.endpoint_id = endpoint.id
					and source_fetch.outcome = 'stored'
					and source_fetch.final_url = parent.canonical_url
					and source_fetch.object_key = parent.object_key
					and source_fetch.raw_sha256 = parent.raw_sha256
					and source_fetch.attempted_at = parent.first_seen_at
					and source_fetch.completed_at = parent.first_fetched_at
			)
		))
		and child.content_policy = 'link-and-excerpt'
		and child.object_key is not null and child.raw_pruned_at is null
		and child.ingestion_error_code is null and parent.ingestion_error_code is null
		and source.enabled and source.validation_state = 'active'
		and source.trust_tier in ('T0', 'T1')
		and item_source.source_tier = source.trust_tier
		and item_source.canonical_url = child.canonical_url
		and not exists (
			select 1 from app.raw_documents newer
			where newer.source_entry_id = child.source_entry_id
				and (newer.first_seen_at, newer.id) > (child.first_seen_at, child.id)
		)`

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadEvidence(ctx context.Context, query rowQuerier, rawDocumentID, revisionID string, lock bool) (evidence, bool, error) {
	statement := evidenceQuery
	if lock {
		statement += ` for update of item for share of child, parent, entry, source, endpoint, revision, item_source`
	}
	var found evidence
	err := query.QueryRow(ctx, statement, rawDocumentID, revisionID).Scan(
		&found.objectKey, &found.rawSHA256, &found.sourceID, &found.canonicalURL,
		&found.entryRowID, &found.entryID, &found.entryFirstSeenAt,
		&found.endpointURL, &found.parentURL,
		&found.title, &found.observedAt, &found.itemID, &found.itemStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return evidence{}, false, nil
	}
	if err != nil {
		return evidence{}, false, fmt.Errorf("load advisory evidence: %w", err)
	}
	if !officialAdvisoryParent(found.endpointURL, found.parentURL) {
		return evidence{}, false, nil
	}
	return found, true, nil
}

func officialAdvisoryParent(endpointURL, parentURL string) bool {
	if parentURL == endpointURL {
		return true
	}
	return endpointURL == sources.GlobalReviewedAdvisoriesURL &&
		sources.IsGlobalAdvisoryPageURL(parentURL)
}

type advisoryEntry struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	ExternalURL string `json:"externalUrl"`
	Title       string `json:"title"`
	Advisory    struct {
		ID              string                `json:"id"`
		Kind            string                `json:"type"`
		State           string                `json:"state"`
		Severity        string                `json:"severity"`
		PublishedAt     string                `json:"publishedAt"`
		ReviewedAt      string                `json:"reviewedAt"`
		WithdrawnAt     string                `json:"withdrawnAt"`
		Vulnerabilities []alert.Vulnerability `json:"vulnerabilities"`
	} `json:"advisory"`
}

func (store *Store) readAdvisory(ctx context.Context, found evidence) (alert.Advisory, string, error) {
	if !officialAdvisoryEndpoint(found.endpointURL) {
		return alert.Advisory{}, "", errors.New("source endpoint is outside official GitHub advisory API")
	}
	if err := storage.ValidateObjectKey(found.objectKey); err != nil {
		return alert.Advisory{}, "", err
	}
	if len(found.rawSHA256) != sha256.Size {
		return alert.Advisory{}, "", errors.New("advisory raw digest has invalid length")
	}
	var digest [sha256.Size]byte
	copy(digest[:], found.rawSHA256)
	identityHash := sha256.Sum256([]byte(found.entryID))
	keySourceID := found.sourceID + "-entry-" + hex.EncodeToString(identityHash[:])
	expectedKey, err := storage.RawObjectKey(keySourceID, found.entryFirstSeenAt, digest, "application/json")
	if err != nil || expectedKey != found.objectKey {
		return alert.Advisory{}, "", errors.New("advisory object key differs from recorded entry and digest")
	}
	payload, err := store.objects.Read(ctx, found.objectKey, maximumAdvisoryEntryBytes)
	if err != nil {
		return alert.Advisory{}, "", fmt.Errorf("read advisory child object: %w", err)
	}
	if len(payload) == 0 || int64(len(payload)) > maximumAdvisoryEntryBytes || sha256.Sum256(payload) != digest {
		return alert.Advisory{}, "", errors.New("advisory child object digest or size differs from record")
	}
	if err := parsing.ValidateEntry(found.endpointURL, parsing.Entry{
		ExternalID: found.entryID, URL: found.canonicalURL, Payload: payload,
	}); err != nil {
		return alert.Advisory{}, "", fmt.Errorf("validate advisory child: %w", err)
	}
	var entry advisoryEntry
	if err := json.Unmarshal(payload, &entry); err != nil {
		return alert.Advisory{}, "", fmt.Errorf("decode advisory metadata: %w", err)
	}
	if entry.ID != found.entryID || entry.URL != found.canonicalURL ||
		!strings.HasPrefix(entry.ID, "github_advisories:") || entry.Advisory.ID == "" {
		return alert.Advisory{}, "", errors.New("advisory child identity is missing or inconsistent")
	}
	sourceURL := entry.URL
	if entry.ExternalURL != "" {
		sourceURL = entry.ExternalURL
	}
	if !officialAdvisoryURL(found.endpointURL, sourceURL, entry.Advisory.ID) {
		return alert.Advisory{}, "", errors.New("advisory public URL is not the matching official GitHub advisory")
	}
	return alert.Advisory{
		ID: entry.Advisory.ID, Kind: entry.Advisory.Kind,
		State: entry.Advisory.State, Severity: entry.Advisory.Severity,
		PublishedAt: entry.Advisory.PublishedAt, ReviewedAt: entry.Advisory.ReviewedAt,
		WithdrawnAt:     entry.Advisory.WithdrawnAt,
		Vulnerabilities: entry.Advisory.Vulnerabilities,
	}, sourceURL, nil
}

func officialAdvisoryEndpoint(raw string) bool {
	if raw == sources.GlobalReviewedAdvisoriesURL {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "api.github.com" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return false
	}
	parts := strings.Split(parsed.Path, "/")
	return len(parts) == 5 && parts[0] == "" && parts[1] == "repos" &&
		parts[2] != "" && parts[3] != "" && parts[4] == "security-advisories" &&
		parts[2] != "." && parts[2] != ".." && parts[3] != "." && parts[3] != ".."
}

func officialAdvisoryURL(endpoint, rawURL, advisoryID string) bool {
	public, err := url.Parse(rawURL)
	if err != nil || public.Scheme != "https" || public.Host != "github.com" ||
		public.User != nil || public.RawQuery != "" || public.Fragment != "" ||
		public.RawPath != "" || advisoryID == "" {
		return false
	}
	api, err := url.Parse(endpoint)
	if err != nil || !officialAdvisoryEndpoint(endpoint) {
		return false
	}
	parts := strings.Split(public.Path, "/")
	if api.Path == "/advisories" {
		return len(parts) == 3 && parts[0] == "" && parts[1] == "advisories" && parts[2] == advisoryID
	}
	apiParts := strings.Split(api.Path, "/")
	return len(parts) == 6 && parts[0] == "" &&
		strings.EqualFold(parts[1], apiParts[2]) &&
		strings.EqualFold(parts[2], apiParts[3]) &&
		parts[3] == "security" && parts[4] == "advisories" && parts[5] == advisoryID
}

type ownerWatches struct {
	userID   string
	watches  []alert.Watch
	quiet    alert.QuietHours
	channels []string
}

func loadOwnerWatches(ctx context.Context, tx pgx.Tx, advisory alert.Advisory) ([]ownerWatches, error) {
	ecosystems := make([]string, 0, len(advisory.Vulnerabilities))
	packages := make([]string, 0, len(advisory.Vulnerabilities))
	for _, vulnerability := range advisory.Vulnerabilities {
		ecosystems = append(ecosystems, vulnerability.Ecosystem)
		packages = append(packages, vulnerability.PackageName)
	}
	rows, err := tx.Query(ctx, `
		select watch.id::text, watch.user_id::text, watch.ecosystem,
			watch.package_name, watch.current_version, watch.status,
			owner.timezone,
			coalesce(to_char(settings.quiet_hours_start, 'HH24:MI'), '22:00'),
			coalesce(to_char(settings.quiet_hours_end, 'HH24:MI'), '07:00'),
			coalesce(settings.critical_alerts_bypass, true),
			coalesce(settings.critical_alert_channels, array['dashboard']::text[])
		from app.watched_technologies watch
		join app.users owner on owner.id = watch.user_id
		left join app.owner_settings settings on settings.user_id = owner.id
		where watch.status = 'active' and watch.ecosystem = any($1::text[])
			and watch.package_name = any($2::text[])
		order by watch.user_id, watch.id
		for share of watch, owner`, ecosystems, packages)
	if err != nil {
		return nil, fmt.Errorf("load affected owner watches: %w", err)
	}
	defer rows.Close()
	owners := make([]ownerWatches, 0)
	for rows.Next() {
		var watch alert.Watch
		var ownerID string
		var quiet alert.QuietHours
		var channels []string
		if err := rows.Scan(&watch.ID, &ownerID, &watch.Ecosystem, &watch.PackageName,
			&watch.CurrentVersion, &watch.Status, &quiet.Timezone, &quiet.Start,
			&quiet.End, &quiet.Bypass, &channels); err != nil {
			return nil, fmt.Errorf("scan affected owner watch: %w", err)
		}
		if len(owners) == 0 || owners[len(owners)-1].userID != ownerID {
			owners = append(owners, ownerWatches{userID: ownerID, quiet: quiet, channels: channels})
		}
		owners[len(owners)-1].watches = append(owners[len(owners)-1].watches, watch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate affected owner watches: %w", err)
	}
	return owners, nil
}

// Assess fails closed for stale or unsupported evidence. A durable alert and
// every selected delivery intent commit atomically with their River jobs.
func (store *Store) Assess(ctx context.Context, rawDocumentID, revisionID string) error {
	preflight, ok, err := loadEvidence(ctx, store.pool, rawDocumentID, revisionID, false)
	if err != nil || !ok {
		return err
	}
	advisory, sourceURL, err := store.readAdvisory(ctx, preflight)
	if err != nil {
		return err
	}
	now := store.clock.Now().UTC()
	if now.IsZero() {
		return errors.New("critical advisory assessment clock is zero")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin critical advisory assessment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The FK key-share lock of a concurrent child insert conflicts with this
	// lock. The evidence query below gets a fresh READ COMMITTED snapshot after
	// any earlier insert finishes, then holds the lock through admission.
	var lockedEntryID string
	err = tx.QueryRow(ctx, `
		select id::text from app.source_entries where id = $1::uuid for update`,
		preflight.entryRowID).Scan(&lockedEntryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock advisory source entry: %w", err)
	}
	current, ok, err := loadEvidence(ctx, tx, rawDocumentID, revisionID, true)
	if err != nil || !ok {
		return err
	}
	if current.objectKey != preflight.objectKey ||
		!equalDigest(current.rawSHA256, preflight.rawSHA256) ||
		current.entryRowID != lockedEntryID ||
		current.entryID != preflight.entryID || current.sourceID != preflight.sourceID ||
		current.canonicalURL != preflight.canonicalURL || current.endpointURL != preflight.endpointURL ||
		current.parentURL != preflight.parentURL ||
		!current.entryFirstSeenAt.Equal(preflight.entryFirstSeenAt) ||
		current.itemID != preflight.itemID || current.itemStatus != preflight.itemStatus ||
		current.title != preflight.title ||
		!current.observedAt.Equal(preflight.observedAt) {
		return errors.New("advisory evidence changed during assessment")
	}
	if current.title == "" || len(current.title) > 500 ||
		sourceURL == "" || len(sourceURL) > 4096 {
		return errors.New("advisory title or source URL exceeds alert bounds")
	}
	if reason := advisoryCorrectionReason(advisory); reason != "" {
		if err := store.suppressCorrectedAlertDeliveries(ctx, tx, current,
			rawDocumentID, revisionID, advisory.ID, reason, now); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if current.itemStatus != "active" && current.itemStatus != "updated" {
		return nil
	}
	owners, err := loadOwnerWatches(ctx, tx, advisory)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		matches := alert.Match(advisory, owner.watches)
		if len(matches) == 0 {
			continue
		}
		scheduledAt, err := alert.DeliveryAt(now, owner.quiet)
		if err != nil {
			return fmt.Errorf("schedule critical alert for owner %s: %w", owner.userID, err)
		}
		for _, match := range matches {
			if err := store.insertMatch(ctx, tx, rawDocumentID, revisionID, current, sourceURL,
				advisory.ID, owner.userID, match, owner.channels, scheduledAt); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit critical advisory assessment: %w", err)
	}
	return nil
}

func advisoryCorrectionReason(advisory alert.Advisory) string {
	if !validCorrectionTimestamp(advisory.PublishedAt) {
		return ""
	}
	if advisory.Kind == "reviewed" {
		if !validCorrectionTimestamp(advisory.ReviewedAt) {
			return ""
		}
	} else if advisory.Kind != "" {
		return ""
	}
	if advisory.WithdrawnAt != "" {
		if !validCorrectionTimestamp(advisory.WithdrawnAt) ||
			(advisory.State != "" && advisory.State != "published" && advisory.State != "closed") ||
			(advisory.Kind == "" && advisory.State != "published" && advisory.State != "closed") {
			return ""
		}
		return "withdrawn"
	}
	if advisory.Kind == "" && advisory.State == "closed" {
		return "no_longer_published"
	}
	if (advisory.State != "" && advisory.State != "published") ||
		(advisory.Kind == "" && advisory.State != "published") {
		return ""
	}
	switch advisory.Severity {
	case "high", "medium", "low":
		return "severity_downgraded"
	case "unknown":
		return "severity_unconfirmed"
	default:
		return ""
	}
}

func validCorrectionTimestamp(value string) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && !parsed.IsZero()
}

func (store *Store) suppressCorrectedAlertDeliveries(ctx context.Context, tx pgx.Tx,
	current evidence, rawDocumentID, revisionID, advisoryID, reason string, now time.Time,
) error {
	_, err := tx.Exec(ctx, `
		update app.critical_alerts admitted
			set correction_reason = $5,
				correction_revision_id = $6::uuid,
				corrected_at = $7::timestamptz
			from app.raw_documents original
			where admitted.raw_document_id = original.id
				and original.source_entry_id = $1::uuid
				and admitted.advisory_id = $2
				and admitted.correction_revision_id is distinct from $6::uuid
				and (original.first_seen_at, original.id) < ($3::timestamptz, $4::uuid)`,
		current.entryRowID, advisoryID, current.observedAt, rawDocumentID,
		reason, revisionID, now.UTC())
	if err != nil {
		return fmt.Errorf("record critical advisory correction: %w", err)
	}
	// Lock every channel row, including an in-flight send. A failure that
	// settles while this transaction waits must be observed before suppression.
	rows, err := tx.Query(ctx, `
		select delivery.id::text
		from app.critical_alert_deliveries delivery
		join app.critical_alerts admitted on admitted.id = delivery.alert_id
		join app.raw_documents original on original.id = admitted.raw_document_id
		where original.source_entry_id = $1::uuid and admitted.advisory_id = $2
			and (original.first_seen_at, original.id) < ($3::timestamptz, $4::uuid)
		for update of delivery`, current.entryRowID, advisoryID, current.observedAt, rawDocumentID)
	if err != nil {
		return fmt.Errorf("lock corrected critical advisory deliveries: %w", err)
	}
	for rows.Next() {
		var deliveryID string
		if err := rows.Scan(&deliveryID); err != nil {
			rows.Close()
			return fmt.Errorf("scan corrected critical advisory delivery: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate corrected critical advisory deliveries: %w", err)
	}
	rows.Close()
	_, err = tx.Exec(ctx, `
		update app.critical_alert_deliveries delivery
		set state = 'suppressed', updated_at = $5::timestamptz
		from app.critical_alerts admitted
		join app.raw_documents original on original.id = admitted.raw_document_id
		where delivery.alert_id = admitted.id
			and original.source_entry_id = $1::uuid and admitted.advisory_id = $2
			and (original.first_seen_at, original.id) < ($3::timestamptz, $4::uuid)
			and delivery.state in ('pending', 'failed')`,
		current.entryRowID, advisoryID, current.observedAt, rawDocumentID, now.UTC())
	if err != nil {
		return fmt.Errorf("suppress corrected critical advisory deliveries: %w", err)
	}
	return nil
}

func equalDigest(first, second []byte) bool {
	if len(first) != sha256.Size || len(second) != sha256.Size {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

func (store *Store) insertMatch(ctx context.Context, tx pgx.Tx,
	rawDocumentID, revisionID string, evidence evidence, sourceURL, advisoryID, userID string,
	match alert.MatchResult, channels []string, scheduledAt time.Time,
) error {
	var alertID string
	err := tx.QueryRow(ctx, `
		insert into app.critical_alerts (
			user_id, advisory_id, ecosystem, package_name, current_version,
			vulnerable_range, patched_version, raw_document_id, revision_id,
			item_id, source_id, source_url, title, observed_at
		) values (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8::uuid, $9::uuid,
			$10::uuid, $11, $12, $13, $14
		)
		on conflict (user_id, advisory_id, ecosystem, package_name) do nothing
		returning id::text`,
		userID, advisoryID, match.Vulnerability.Ecosystem, match.Vulnerability.PackageName,
		match.Watch.CurrentVersion, match.Vulnerability.VersionRange,
		match.Vulnerability.PatchedVersion, rawDocumentID, revisionID,
		evidence.itemID, evidence.sourceID, sourceURL,
		evidence.title, evidence.observedAt,
	).Scan(&alertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // The first admission owns the immutable snapshot and channels.
	}
	if err != nil {
		return fmt.Errorf("insert confirmed critical alert: %w", err)
	}
	for _, channel := range channels {
		if channel == "dashboard" {
			continue
		}
		if channel != "discord" && channel != "email" {
			return fmt.Errorf("unsupported critical alert channel %q", channel)
		}
		idempotencyKey := "critical-alert:" + alertID + ":" + channel
		delivery := alert.Delivery{
			AlertID: alertID, Channel: channel, IdempotencyKey: idempotencyKey,
			Title: evidence.title, SourceURL: sourceURL,
			PackageName:    match.Vulnerability.PackageName,
			Ecosystem:      match.Vulnerability.Ecosystem,
			CurrentVersion: match.Watch.CurrentVersion,
			VersionRange:   match.Vulnerability.VersionRange,
			PatchedVersion: match.Vulnerability.PatchedVersion,
		}
		payloadSHA256 := alert.DeliverySHA256(delivery)
		var deliveryID string
		if err := tx.QueryRow(ctx, `
			insert into app.critical_alert_deliveries (
				alert_id, channel, idempotency_key, payload_sha256, next_attempt_at
			) values ($1::uuid, $2, $3, $4, $5)
			returning id::text`, alertID, channel, idempotencyKey,
			payloadSHA256[:], scheduledAt).Scan(&deliveryID); err != nil {
			return fmt.Errorf("insert critical alert delivery intent: %w", err)
		}
		if _, _, err := store.jobs.EnqueueDeliverCriticalAlert(ctx, tx,
			jobqueue.DeliverCriticalAlertArgs{DeliveryID: deliveryID}, scheduledAt); err != nil {
			return err
		}
	}
	return nil
}
