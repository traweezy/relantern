import type { Metadata } from "next";
import { RadarWorkspace } from "@/features/control-plane/radar-workspace";
import { requireOwnerSession } from "@/server/auth/session";
import { getRadar } from "@/server/controlplane/client";

export const metadata: Metadata = { title: "Radar | Relantern" };

const RadarPage = async () => {
  const owner = await requireOwnerSession();
  const snapshot = await getRadar(owner.userID).catch(() => null);
  return (
    <>
      <header className="intelligence-header control-plane-header">
        <div>
          <p className="eyebrow">Radar · Owner decision ledger</p>
          <h1>Compare change before you invite it in.</h1>
          <p>
            Review stability, maintenance, security, performance, migration cost, and reversible
            exit paths. Relantern can assess evidence; only you can adopt.
          </p>
        </div>
        {snapshot !== null && (
          <div className="control-plane-stamp">
            <span>Review queue</span>
            <strong>
              {snapshot.candidates.length} candidates · {snapshot.reviewDue} due
            </strong>
          </div>
        )}
      </header>
      {snapshot === null ? (
        <div className="reading-empty-state">
          <p className="eyebrow">Radar read degraded</p>
          <h2>The evidence ledger is temporarily unavailable.</h2>
          <p>The private API failed closed and no cached adoption guidance is shown.</p>
        </div>
      ) : (
        <RadarWorkspace initialSnapshot={snapshot} timezone={owner.timezone} />
      )}
    </>
  );
};

export default RadarPage;
