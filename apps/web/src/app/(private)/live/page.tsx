import type { Metadata } from "next";
import { EmptyState } from "@/features/intelligence/empty-state";
import { LiveStream } from "@/features/intelligence/live-stream";
import { requireOwnerSession } from "@/server/auth/session";
import { getLiveSnapshot } from "@/server/intelligence/client";

export const metadata: Metadata = {
  title: "Live | Relantern",
};

const LivePage = async () => {
  const owner = await requireOwnerSession();
  const snapshot = await getLiveSnapshot().catch(() => null);
  return (
    <>
      <header className="intelligence-header">
        <div>
          <p className="eyebrow">Live · Continuous evidence</p>
          <h1>Watch material change arrive.</h1>
          <p>
            A resumable owner stream for evidence that has cleared deterministic collection and
            provenance checks.
          </p>
        </div>
        <span className="health-pill">Seven-day resume window</span>
      </header>
      <section aria-labelledby="live-stream-title" className="brief-section">
        <div className="collection-toolbar">
          <div>
            <p className="eyebrow">No forced scroll</p>
            <h2 id="live-stream-title">Intelligence stream</h2>
          </div>
          <p>Pause at any point; new evidence waits above your current reading position.</p>
        </div>
        {snapshot === null ? (
          <EmptyState
            detail="The private stream is fail-closed until its API dependency recovers."
            eyebrow="Read path degraded"
            title="Live intelligence is temporarily unavailable"
          />
        ) : snapshot.events.length === 0 ? (
          <EmptyState
            detail="The stream is connected. Material events will appear after evidence checks pass."
            eyebrow="Listening"
            title="No events in the current window"
          />
        ) : (
          <LiveStream initialSnapshot={snapshot} timezone={owner.timezone} />
        )}
      </section>
    </>
  );
};

export default LivePage;
