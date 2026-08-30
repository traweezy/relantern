import type { OperationsSnapshot, QueueDepth } from "@relantern/domain";
import { memo } from "react";

type OperationsDashboardProps = Readonly<{
  operations: OperationsSnapshot;
  timezone: string;
}>;

const formatTime = (value: string | undefined, timezone: string): string => {
  if (value === undefined) return "Not recorded";
  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone,
  }).format(new Date(value));
};

const activeDepth = (queue: QueueDepth): number =>
  queue.available + queue.retryable + queue.running + queue.scheduled;

const OperationsDashboardComponent = ({ operations, timezone }: OperationsDashboardProps) => {
  const totalActive = operations.queues.reduce((total, queue) => total + activeDepth(queue), 0);
  const failedSources = operations.sourceErrorBudgets.filter(
    (budget) => budget.state === "exhausted" || budget.state === "warning",
  ).length;
  return (
    <div className="operations-workspace">
      <section aria-label="Operations status" className="operations-summary-grid">
        <article>
          <span>Active jobs</span>
          <strong>{totalActive}</strong>
          <p>Available, retryable, running, or scheduled</p>
        </article>
        <article>
          <span>Stuck jobs</span>
          <strong>{operations.stuckJobs.length}</strong>
          <p>Running longer than the two-minute rescue threshold</p>
        </article>
        <article>
          <span>OpenAI pending</span>
          <strong>{operations.openaiBackgroundPending}</strong>
          <p>Background responses awaiting reconciliation</p>
        </article>
        <article>
          <span>Source budgets</span>
          <strong>{failedSources}</strong>
          <p>Warning or exhausted in the last 24 hours</p>
        </article>
        <article>
          <span>Delivery attempts</span>
          <strong>{operations.deliveryAttempts}</strong>
          <p>Delivery ledger activates in PR17</p>
        </article>
      </section>

      <section aria-labelledby="queue-depth-title" className="operations-panel">
        <div className="collection-toolbar">
          <div>
            <p className="eyebrow">Backpressure</p>
            <h2 id="queue-depth-title">Queue depth</h2>
          </div>
          <p>Bounded counts by River queue and lifecycle state.</p>
        </div>
        <div className="operations-table-wrap">
          <table className="operations-table">
            <caption>River queue depth by state</caption>
            <thead>
              <tr>
                <th scope="col">Queue</th>
                <th scope="col">Active</th>
                <th scope="col">Running</th>
                <th scope="col">Retryable</th>
                <th scope="col">Completed</th>
                <th scope="col">Discarded</th>
              </tr>
            </thead>
            <tbody>
              {operations.queues.map((queue) => (
                <tr key={queue.queue}>
                  <th scope="row">{queue.queue}</th>
                  <td>{activeDepth(queue)}</td>
                  <td>{queue.running}</td>
                  <td>{queue.retryable}</td>
                  <td>{queue.completed}</td>
                  <td>{queue.discarded}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {operations.stuckJobs.length > 0 && (
          <div className="stuck-job-list">
            {operations.stuckJobs.map((job) => (
              <article key={job.id}>
                <strong>{job.kind}</strong>
                <span>
                  {job.queue} · attempt {job.attempt}
                </span>
                <span>{formatTime(job.attemptedAt, timezone)}</span>
              </article>
            ))}
          </div>
        )}
      </section>

      <section aria-labelledby="source-budget-title" className="operations-panel">
        <div className="collection-toolbar">
          <div>
            <p className="eyebrow">24-hour collection SLO</p>
            <h2 id="source-budget-title">Source error budgets</h2>
          </div>
          <p>Healthy is at most 5% failed attempts; warning is at most 20%.</p>
        </div>
        <div className="source-budget-grid">
          {operations.sourceErrorBudgets.map((budget) => (
            <article
              className={`source-budget source-budget-${budget.state}`}
              key={budget.sourceId}
            >
              <span>{budget.state}</span>
              <strong>{budget.sourceName}</strong>
              <p>
                {budget.failures} failed of {budget.attempts} ·{" "}
                {Math.round(budget.failureRate * 100)}%
              </p>
            </article>
          ))}
        </div>
      </section>

      <section aria-labelledby="schedule-ledger-title" className="operations-panel">
        <div className="collection-toolbar">
          <div>
            <p className="eyebrow">Durable timing authority</p>
            <h2 id="schedule-ledger-title">Schedule occurrence ledger</h2>
          </div>
          <div className="operations-health-copy">
            <span>
              Last reconciler success {formatTime(operations.schedulerLastSuccess, timezone)}
            </span>
            <span>Oldest overdue {formatTime(operations.oldestOverdueOccurrence, timezone)}</span>
          </div>
        </div>
        <div className="operations-table-wrap">
          <table className="operations-table occurrence-table">
            <caption>Most recent schedule occurrences and duplicate-suppression keys</caption>
            <thead>
              <tr>
                <th scope="col">Schedule</th>
                <th scope="col">Local date</th>
                <th scope="col">Trigger</th>
                <th scope="col">State</th>
                <th scope="col">Scheduled</th>
                <th scope="col">Idempotency</th>
              </tr>
            </thead>
            <tbody>
              {operations.occurrences.length === 0 ? (
                <tr>
                  <td colSpan={6}>No occurrences have been recorded for this owner.</td>
                </tr>
              ) : (
                operations.occurrences.map((occurrence) => (
                  <tr key={occurrence.id}>
                    <th scope="row">{occurrence.scheduleType.replace("_", " ")}</th>
                    <td>{occurrence.localDate}</td>
                    <td>{occurrence.triggerType.replace("_", " ")}</td>
                    <td>{occurrence.state}</td>
                    <td>{formatTime(occurrence.scheduledFor, timezone)}</td>
                    <td>
                      <code title={occurrence.idempotencyKey}>
                        {occurrence.idempotencyKey.slice(0, 18)}…
                      </code>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      <section aria-label="Deployment and restore state" className="operations-provenance-grid">
        <article>
          <p className="eyebrow">Current deployment</p>
          <h2>{operations.deployment.environment}</h2>
          <dl>
            <div>
              <dt>Version</dt>
              <dd>{operations.deployment.version}</dd>
            </div>
            <div>
              <dt>Git SHA</dt>
              <dd>
                <code>{operations.deployment.gitSha}</code>
              </dd>
            </div>
            <div>
              <dt>Snapshot</dt>
              <dd>{formatTime(operations.generatedAt, timezone)}</dd>
            </div>
          </dl>
        </article>
        <article>
          <p className="eyebrow">Restore evidence</p>
          <h2>{operations.restore.state.replace("_", " ")}</h2>
          <p>{operations.restore.explanation}</p>
        </article>
      </section>
    </div>
  );
};

export const OperationsDashboard = memo<OperationsDashboardProps>(OperationsDashboardComponent);
OperationsDashboard.displayName = "OperationsDashboard";
