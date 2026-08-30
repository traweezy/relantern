import type { Metadata } from "next";
import { SourceRegistry } from "@/features/control-plane/source-registry";
import { requireOwnerSession } from "@/server/auth/session";
import { getSources } from "@/server/controlplane/client";

export const metadata: Metadata = { title: "Sources | Relantern" };

const SourcesPage = async () => {
  const owner = await requireOwnerSession();
  const snapshot = await getSources(owner.userID).catch(() => null);
  return (
    <>
      <header className="intelligence-header control-plane-header">
        <div>
          <p className="eyebrow">Sources · Collection boundary</p>
          <h1>See what the lantern can trust.</h1>
          <p>
            Inspect cadence, endpoint health, error budgets, duplicate pressure, and owner controls
            without coupling ranking preferences to collection.
          </p>
        </div>
        {snapshot !== null && (
          <div className="control-plane-stamp">
            <span>Registry snapshot</span>
            <strong>{snapshot.sources.length} sources</strong>
          </div>
        )}
      </header>
      {snapshot === null ? (
        <div className="reading-empty-state">
          <p className="eyebrow">Control plane degraded</p>
          <h2>Source health is temporarily unavailable.</h2>
          <p>The private API failed closed; no stale operational fallback is shown.</p>
        </div>
      ) : (
        <SourceRegistry initialSources={snapshot.sources} timezone={owner.timezone} />
      )}
    </>
  );
};

export default SourcesPage;
