import type { Metadata } from "next";
import { OperationsDashboard } from "@/features/control-plane/operations-dashboard";
import { requireOwnerSession } from "@/server/auth/session";
import { getOperations } from "@/server/controlplane/client";

export const metadata: Metadata = { title: "Operations | Relantern" };

const OperationsPage = async () => {
  const owner = await requireOwnerSession();
  const operations = await getOperations(owner.userID).catch(() => null);
  return (
    <>
      <header className="intelligence-header control-plane-header">
        <div>
          <p className="eyebrow">Operations · Private telemetry</p>
          <h1>Know what is waiting, late, or unsafe.</h1>
          <p>
            Queue backpressure, source error budgets, OpenAI reconciliation, schedule history,
            deploy identity, and restore evidence in one bounded read.
          </p>
        </div>
        {operations !== null && (
          <div className="control-plane-stamp">
            <span>Environment</span>
            <strong>{operations.deployment.environment}</strong>
          </div>
        )}
      </header>
      {operations === null ? (
        <div className="reading-empty-state">
          <p className="eyebrow">Operations read degraded</p>
          <h2>The operational snapshot is unavailable.</h2>
          <p>The owner-only endpoint failed closed and does not expose a cached fallback.</p>
        </div>
      ) : (
        <OperationsDashboard operations={operations} timezone={owner.timezone} />
      )}
    </>
  );
};

export default OperationsPage;
