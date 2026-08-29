import type { Metadata } from "next";
import { EmptyState } from "@/features/intelligence/empty-state";
import { StoryCard } from "@/features/intelligence/story-card";
import { requireOwnerSession } from "@/server/auth/session";
import { getTodaySnapshot } from "@/server/intelligence/client";

export const metadata: Metadata = {
  title: "Today | Relantern",
};

const formatCoverage = (start: string, end: string, timezone: string): string => {
  const formatter = new Intl.DateTimeFormat("en-US", {
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    month: "short",
    timeZone: timezone,
  });
  return `${formatter.format(new Date(start))} – ${formatter.format(new Date(end))}`;
};

const TodayPage = async () => {
  const owner = await requireOwnerSession();
  const snapshot = await getTodaySnapshot().catch(() => null);

  if (snapshot === null) {
    return (
      <>
        <header className="intelligence-header">
          <div>
            <p className="eyebrow">Today · Private brief</p>
            <h1>Signal for the work ahead.</h1>
            <p>The evidence pipeline is healthy, but the current snapshot could not be loaded.</p>
          </div>
          <span className="health-pill health-degraded">Read path degraded</span>
        </header>
        <EmptyState
          detail="The private API remains fail-closed. Retry after its readiness check recovers."
          eyebrow="No unsafe fallback"
          title="Today is temporarily unavailable"
        />
      </>
    );
  }

  const generatedAt = new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: owner.timezone,
  }).format(new Date(snapshot.generatedAt));

  return (
    <>
      <header className="intelligence-header">
        <div>
          <p className="eyebrow">Today · Private brief</p>
          <h1>Signal for the work ahead.</h1>
          <p>
            {snapshot.stories.length} evidence-backed stories across the last 24 hours, ordered for
            action rather than attention.
          </p>
        </div>
        <div className="header-actions">
          <button disabled title="Delivery controls arrive in the delivery slice" type="button">
            Preview digest
          </button>
          <button disabled title="Delivery controls arrive in the delivery slice" type="button">
            Run now
          </button>
        </div>
      </header>

      <section aria-label="Daily intelligence status" className="brief-status-grid">
        <article className="brief-status brief-status-primary">
          <span>Critical</span>
          <strong>{snapshot.stats.criticalAlerts}</strong>
          <p>Confirmed watched-dependency alerts</p>
        </article>
        <article className="brief-status">
          <span>Releases</span>
          <strong>{snapshot.stats.releases}</strong>
          <p>Material version changes</p>
        </article>
        <article className="brief-status">
          <span>Coverage</span>
          <strong>{snapshot.stats.sourceCoverage}%</strong>
          <p>Enabled sources reporting healthy</p>
        </article>
        <article className="brief-status">
          <span>Review</span>
          <strong>{snapshot.stats.reviewRequired}</strong>
          <p>Claims waiting for owner judgment</p>
        </article>
        <article className="brief-status">
          <span>AI cost</span>
          <strong>${snapshot.stats.estimatedCostUsd}</strong>
          <p>Current calendar month</p>
        </article>
      </section>

      <section aria-labelledby="brief-title" className="brief-section">
        <div className="collection-toolbar">
          <div>
            <p className="eyebrow">Evidence window</p>
            <h2 id="brief-title">Morning brief</h2>
          </div>
          <div className="coverage-copy">
            <span>
              {formatCoverage(snapshot.coverageStartAt, snapshot.coverageEndAt, owner.timezone)}
            </span>
            <span>Generated {generatedAt}</span>
            <span>Delivery {snapshot.deliveryState}</span>
          </div>
        </div>
        {snapshot.stories.length === 0 ? (
          <EmptyState
            detail="Continuous ingestion is active. New material stories will appear after evidence and provenance checks pass."
            eyebrow="Coverage complete"
            title="No material changes in this window"
          />
        ) : (
          <div className="story-list">
            {snapshot.stories.map((story) => (
              <StoryCard
                href={`/story/${story.id}`}
                key={story.id}
                story={story}
                timezone={owner.timezone}
              />
            ))}
          </div>
        )}
      </section>
    </>
  );
};

export default TodayPage;
