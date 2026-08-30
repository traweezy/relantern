import type { Metadata } from "next";
import { DigestQuickControls } from "@/features/control-plane/digest-quick-controls";
import { EmptyState } from "@/features/intelligence/empty-state";
import { TriageCollection } from "@/features/reading-state/triage-collection";
import { requireOwnerSession } from "@/server/auth/session";
import { getSettings } from "@/server/controlplane/client";
import { getTodaySnapshot } from "@/server/intelligence/client";
import { getStoryStates, getTags } from "@/server/reading-state/client";

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
  const [snapshot, settings] = await Promise.all([
    getTodaySnapshot().catch(() => null),
    getSettings(owner.userID).catch(() => null),
  ]);

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
  const [states, tags] =
    snapshot.stories.length === 0
      ? ([[], []] as const)
      : await Promise.all([
          getStoryStates(
            owner.userID,
            snapshot.stories.map((story) => story.id),
          ),
          getTags(owner.userID),
        ]);
  const stateByStoryID = new Map(states.map((state) => [state.storyId, state] as const));
  const now = Date.now();
  const activeStories = snapshot.stories.flatMap((story) => {
    const state = stateByStoryID.get(story.id);
    if (
      state === undefined ||
      state.location === "archive" ||
      (state.snoozedUntil !== null && Date.parse(state.snoozedUntil) > now)
    ) {
      return [];
    }
    return [{ state, story }];
  });
  const dailySchedule = settings?.schedules.find(
    (schedule) => schedule.scheduleType === "daily_digest",
  );

  return (
    <>
      <header className="intelligence-header">
        <div>
          <p className="eyebrow">Today · Private brief</p>
          <h1>Signal for the work ahead.</h1>
          <p>
            {activeStories.length} evidence-backed stories across the last 24 hours, ordered for
            action rather than attention.
          </p>
        </div>
        {dailySchedule !== undefined && <DigestQuickControls scheduleID={dailySchedule.id} />}
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
        {activeStories.length === 0 ? (
          <EmptyState
            detail="Continuous ingestion is active. New material stories will appear after evidence and provenance checks pass."
            eyebrow="Coverage complete"
            title="No material changes in this window"
          />
        ) : (
          <TriageCollection
            initialCollection={{
              items: activeStories,
              nextCursor: null,
              total: activeStories.length,
            }}
            kind="today"
            tags={tags}
            timezone={owner.timezone}
          />
        )}
      </section>
    </>
  );
};

export default TodayPage;
