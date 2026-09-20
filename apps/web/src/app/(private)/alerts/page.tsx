import type { Metadata, Route } from "next";
import Link from "next/link";
import { CriticalAlertCard } from "@/features/intelligence/critical-alert-card";
import { EmptyState } from "@/features/intelligence/empty-state";
import { requireOwnerSession } from "@/server/auth/session";
import { getAlertHistory, IntelligenceResponseError } from "@/server/intelligence/client";

type PageProps = Readonly<{ searchParams: Promise<Readonly<{ cursor?: string }>> }>;

export const metadata: Metadata = { title: "Alert history | Relantern" };

const AlertsPage = async ({ searchParams }: PageProps) => {
  const owner = await requireOwnerSession();
  const cursor = (await searchParams).cursor;
  let history: Awaited<ReturnType<typeof getAlertHistory>> | null = null;
  let invalidCursor = false;
  try {
    history = await getAlertHistory(owner.userID, cursor);
  } catch (error) {
    invalidCursor = error instanceof IntelligenceResponseError && error.status === 400;
  }

  return (
    <>
      <header className="intelligence-header alerts-header">
        <div>
          <p className="eyebrow">Alert history · Owner only</p>
          <h1>Confirmed dependency alerts.</h1>
          <p>
            Review alerts issued for watched package versions, later advisory corrections, and the
            status of each enabled delivery channel.
          </p>
        </div>
      </header>

      {history === null ? (
        <>
          <EmptyState
            detail={
              invalidCursor
                ? "The page link has expired or is invalid. Start again with the newest alerts."
                : "The private alert API could not be loaded. Retry after its readiness check recovers."
            }
            eyebrow={invalidCursor ? "Invalid page link" : "Read path degraded"}
            title={
              invalidCursor
                ? "This alert page is unavailable"
                : "Alert history is temporarily unavailable"
            }
          />
          <Link className="secondary-button alert-history-home-link" href="/alerts">
            View newest alerts
          </Link>
        </>
      ) : history.alerts.length === 0 ? (
        <>
          <EmptyState
            detail={
              cursor === undefined
                ? "Confirmed watched-dependency advisories will appear here after evidence and version checks pass."
                : "There are no older alerts on this page. Return to the newest alerts."
            }
            eyebrow="No matching records"
            title={cursor === undefined ? "No confirmed alerts yet" : "No older alerts"}
          />
          {cursor !== undefined && (
            <Link className="secondary-button alert-history-home-link" href="/alerts">
              View newest alerts
            </Link>
          )}
        </>
      ) : (
        <section aria-labelledby="alert-history-title" className="alert-history">
          <div className="collection-toolbar">
            <div>
              <p className="eyebrow">Newest first</p>
              <h2 id="alert-history-title">Alert and correction history</h2>
            </div>
            <p>{history.alerts.length} on this page</p>
          </div>
          <div className="alert-history-list">
            {history.alerts.map((alert) => (
              <CriticalAlertCard
                alert={alert}
                deliveries={alert.deliveries}
                key={alert.id}
                timezone={owner.timezone}
              />
            ))}
          </div>
          <nav aria-label="Alert history pages" className="alert-history-pagination">
            {cursor !== undefined && <Link href="/alerts">View newest alerts</Link>}
            {history.nextCursor !== null && (
              <Link
                className="secondary-button"
                href={`/alerts?cursor=${encodeURIComponent(history.nextCursor)}` as Route}
              >
                Older alerts
              </Link>
            )}
          </nav>
        </section>
      )}
    </>
  );
};

export default AlertsPage;
