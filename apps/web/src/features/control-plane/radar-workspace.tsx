"use client";

import type {
  RadarCandidate,
  RadarDiscoveryRun,
  RadarSnapshot,
  RadarState,
} from "@relantern/domain";
import { useForm } from "@tanstack/react-form";
import type { ChangeEvent, FormEvent } from "react";
import { memo, useCallback, useMemo, useState } from "react";
import { radarDecisionDraftSchema } from "@/features/control-plane/commands";
import { parseRadarCandidate, parseRadarDiscoveryRun } from "@/features/control-plane/contract";

type RadarWorkspaceProps = Readonly<{
  initialSnapshot: RadarSnapshot;
  timezone: string;
}>;

type CandidateCardProps = Readonly<{
  candidate: RadarCandidate;
  onChange: (candidate: RadarCandidate) => void;
  timezone: string;
}>;

const candidatesPerPage = 3;
const radarStates: readonly RadarState[] = ["adopt", "trial", "assess", "hold", "reject"];

const requestJSON = async <T,>(
  path: string,
  body: unknown,
  parse: (value: unknown) => T,
): Promise<T> => {
  const response = await fetch(path, {
    body: JSON.stringify(body),
    headers: { accept: "application/json", "content-type": "application/json" },
    method: "POST",
    signal: AbortSignal.timeout(15_000),
  });
  const decoded: unknown = await response.json();
  if (!response.ok) {
    const detail =
      typeof decoded === "object" && decoded !== null && "detail" in decoded
        ? String(decoded.detail)
        : "The Radar request failed.";
    throw new Error(detail);
  }
  return parse(decoded);
};

const formatTime = (value: string, timezone: string): string =>
  new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone,
  }).format(new Date(value));

const reviewDateFromNow = (): string => {
  const value = new Date();
  value.setUTCDate(value.getUTCDate() + 30);
  return value.toISOString().slice(0, 10);
};

const splitLines = (value: string): string[] =>
  value
    .split("\n")
    .map((part) => part.trim())
    .filter((part, index, values) => part.length > 0 && values.indexOf(part) === index);

const metricValue = (record: Readonly<Record<string, unknown>>, key: string): string => {
  const value = record[key];
  if (typeof value === "number" || typeof value === "string") {
    return String(value);
  }
  if (typeof value === "boolean") {
    return value ? "yes" : "no";
  }
  return "not observed";
};

const CandidateCardComponent = ({ candidate, onChange, timezone }: CandidateCardProps) => {
  const [notice, setNotice] = useState("No package changes without an explicit owner decision.");
  const decisionForm = useForm({
    defaultValues: {
      compatibilityRequirements:
        candidate.decisions[0]?.compatibilityRequirements.join("\n") ?? "Verify supported runtimes",
      exitConditions:
        candidate.decisions[0]?.exitConditions.join("\n") ?? "Restore the incumbent adapter",
      projectTypes: candidate.decisions[0]?.applicableProjectTypes.join("\n") ?? "web",
      rationale: "Owner review of the latest attributed evidence.",
      reviewDate: reviewDateFromNow(),
      state: candidate.currentState,
    },
    validators: { onBlur: radarDecisionDraftSchema, onSubmit: radarDecisionDraftSchema },
    onSubmit: async ({ value }) => {
      const comparisonID = candidate.latestComparison?.id;
      const updated = await requestJSON(
        `/api/radar/candidates/${encodeURIComponent(candidate.id)}/decisions`,
        {
          applicableProjectTypes: splitLines(value.projectTypes),
          compatibilityRequirements: splitLines(value.compatibilityRequirements),
          evidence: {
            ...(comparisonID === undefined ? {} : { comparisonId: comparisonID }),
            repositoryUrl: candidate.repositoryUrl,
          },
          exitConditions: splitLines(value.exitConditions),
          expectedVersion: candidate.version,
          rationale: value.rationale.trim(),
          reviewAt: new Date(`${value.reviewDate}T12:00:00Z`).toISOString(),
          state: value.state,
        },
        parseRadarCandidate,
      );
      onChange(updated);
      setNotice(`Owner decision saved as ${updated.currentState}; review date recorded.`);
    },
  });

  const handleSubmit = useCallback(
    (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void decisionForm.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The Radar decision was not saved.");
      });
    },
    [decisionForm],
  );

  const comparison = candidate.latestComparison;
  const metric = candidate.latestMetric;
  return (
    <article className="radar-candidate-card">
      <header>
        <div>
          <div className="story-badges">
            <span className={`radar-state radar-state-${candidate.currentState}`}>
              {candidate.currentState}
            </span>
            <span className="signal-badge">{candidate.ecosystem}</span>
            {comparison?.misleading === true && (
              <span className="radar-warning">Popularity conflicts with risk evidence</span>
            )}
          </div>
          <h2>{candidate.packageName}</h2>
          <p>
            Candidate for <strong>{candidate.incumbentPackage}</strong> · discovered through{" "}
            {candidate.discoverySource}
          </p>
        </div>
        <div className="source-state-stack">
          <strong>Review {formatTime(candidate.reviewAt, timezone)}</strong>
          <span>Version {candidate.version}</span>
        </div>
      </header>

      <dl className="radar-metric-grid">
        <div>
          <dt>Release</dt>
          <dd>{metric?.releaseVersion ?? "Pending"}</dd>
        </div>
        <div>
          <dt>License</dt>
          <dd>{metric?.license ?? "Pending"}</dd>
        </div>
        <div>
          <dt>Contributors</dt>
          <dd>{metric?.contributorCount ?? "—"}</dd>
        </div>
        <div>
          <dt>Critical advisories</dt>
          <dd>
            {metric === undefined ? "—" : metricValue(metric.security, "criticalAdvisoryCount")}
          </dd>
        </div>
        <div>
          <dt>Scorecard</dt>
          <dd>{metric === undefined ? "—" : metricValue(metric.security, "scorecardScore")}</dd>
        </div>
        <div>
          <dt>Provenance</dt>
          <dd>{metric === undefined ? "—" : metricValue(metric.provenance, "verified")}</dd>
        </div>
      </dl>

      {comparison === undefined ? (
        <div className="radar-awaiting">
          <strong>Assessment pending</strong>
          <span>No comparison is shown until attributable evidence has been processed.</span>
        </div>
      ) : (
        <details className="radar-comparison">
          <summary>
            Seven-dimension comparison · suggested {comparison.suggestedState} ·{" "}
            {Math.round(comparison.confidence * 100)}% evidence completeness
          </summary>
          <div className="radar-table-wrap">
            <table className="radar-table">
              <caption>
                {candidate.packageName} compared with {candidate.incumbentPackage}
              </caption>
              <thead>
                <tr>
                  <th scope="col">Dimension</th>
                  <th scope="col">Candidate</th>
                  <th scope="col">Current</th>
                  <th scope="col">Evidence</th>
                </tr>
              </thead>
              <tbody>
                {comparison.dimensions.map((dimension) => (
                  <tr key={dimension.name}>
                    <th scope="row">
                      {dimension.name}
                      <small>{dimension.verdict.replace("-", " ")}</small>
                    </th>
                    <td>{dimension.candidate}</td>
                    <td>{dimension.current}</td>
                    <td>
                      {dimension.evidence.slice(0, 2).map((link) => (
                        <a
                          href={link.url}
                          key={`${dimension.name}-${link.url}`}
                          rel="noreferrer"
                          target="_blank"
                        >
                          {link.sourceTier} {link.label}
                        </a>
                      ))}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      )}

      <details className="radar-history">
        <summary>Decision history · {candidate.decisions.length} entries</summary>
        <ol>
          {candidate.decisions.map((decision) => (
            <li key={decision.id}>
              <div>
                <span className={`radar-state radar-state-${decision.state}`}>
                  {decision.state}
                </span>
                <strong>{decision.decisionSource}</strong>
                <time dateTime={decision.decidedAt}>
                  {formatTime(decision.decidedAt, timezone)}
                </time>
              </div>
              <p>{decision.rationale}</p>
              <small>
                Review {formatTime(decision.reviewAt, timezone)} · Exit:{" "}
                {decision.exitConditions.join("; ")}
              </small>
            </li>
          ))}
        </ol>
      </details>

      <details className="radar-decision">
        <summary>Record or revise owner decision</summary>
        <form className="radar-decision-form" onSubmit={handleSubmit}>
          <div className="radar-decision-heading">
            <div>
              <p className="eyebrow">Owner decision</p>
              <strong>Record a bounded state and its exit contract.</strong>
            </div>
            <decisionForm.Field name="state">
              {(field) => (
                <label>
                  State
                  <select
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value as RadarState)}
                    value={field.state.value}
                  >
                    {radarStates.map((state) => (
                      <option key={state} value={state}>
                        {state}
                      </option>
                    ))}
                  </select>
                </label>
              )}
            </decisionForm.Field>
            <decisionForm.Field name="reviewDate">
              {(field) => (
                <label>
                  Review date
                  <input
                    min={new Date().toISOString().slice(0, 10)}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    type="date"
                    value={field.state.value}
                  />
                </label>
              )}
            </decisionForm.Field>
          </div>
          <decisionForm.Field name="rationale">
            {(field) => (
              <label>
                Rationale
                <textarea
                  aria-invalid={field.state.meta.errors.length > 0}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  value={field.state.value}
                />
              </label>
            )}
          </decisionForm.Field>
          <div className="radar-decision-context">
            <decisionForm.Field name="projectTypes">
              {(field) => (
                <label>
                  Applicable project types · one per line
                  <textarea
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    value={field.state.value}
                  />
                </label>
              )}
            </decisionForm.Field>
            <decisionForm.Field name="compatibilityRequirements">
              {(field) => (
                <label>
                  Compatibility requirements · one per line
                  <textarea
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    value={field.state.value}
                  />
                </label>
              )}
            </decisionForm.Field>
            <decisionForm.Field name="exitConditions">
              {(field) => (
                <label>
                  Exit conditions · one per line
                  <textarea
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    value={field.state.value}
                  />
                </label>
              )}
            </decisionForm.Field>
          </div>
          <div className="radar-save-bar">
            <p aria-live="polite">{notice}</p>
            <decisionForm.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
              {([canSubmit, isSubmitting]) => (
                <button disabled={!canSubmit || isSubmitting} type="submit">
                  {isSubmitting ? "Recording decision…" : "Record owner decision"}
                </button>
              )}
            </decisionForm.Subscribe>
          </div>
        </form>
      </details>
    </article>
  );
};

const CandidateCard = memo<CandidateCardProps>(CandidateCardComponent);
CandidateCard.displayName = "CandidateCard";

const RadarWorkspaceComponent = ({ initialSnapshot, timezone }: RadarWorkspaceProps) => {
  const [candidates, setCandidates] = useState(initialSnapshot.candidates);
  const [runs, setRuns] = useState(initialSnapshot.runs);
  const [query, setQuery] = useState("");
  const [stateFilter, setStateFilter] = useState<RadarState | "all">("all");
  const [page, setPage] = useState(1);
  const [discoveryPending, setDiscoveryPending] = useState(false);
  const [notice, setNotice] = useState(
    initialSnapshot.runs[0] === undefined
      ? "Discovery reads only evidence already ingested into the private ledger."
      : `Latest run: ${initialSnapshot.runs[0].state}.`,
  );
  const stateCounts = useMemo(
    () =>
      Object.fromEntries(
        radarStates.map((state) => [
          state,
          candidates.filter((candidate) => candidate.currentState === state).length,
        ]),
      ) as Readonly<Record<RadarState, number>>,
    [candidates],
  );
  const filtered = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    return candidates.filter(
      (candidate) =>
        (stateFilter === "all" || candidate.currentState === stateFilter) &&
        (normalized === "" ||
          candidate.packageName.toLowerCase().includes(normalized) ||
          candidate.incumbentPackage.toLowerCase().includes(normalized) ||
          candidate.ecosystem.includes(normalized)),
    );
  }, [candidates, query, stateFilter]);
  const maximumPage = Math.max(1, Math.ceil(filtered.length / candidatesPerPage));
  const visible = useMemo(
    () => filtered.slice((page - 1) * candidatesPerPage, page * candidatesPerPage),
    [filtered, page],
  );

  const handleCandidateChange = useCallback((updated: RadarCandidate) => {
    setCandidates((current) =>
      current.map((candidate) => (candidate.id === updated.id ? updated : candidate)),
    );
  }, []);
  const handleQueryChange = useCallback((event: ChangeEvent<HTMLInputElement>) => {
    setQuery(event.target.value);
    setPage(1);
  }, []);
  const clearQuery = useCallback(() => {
    setQuery("");
    setPage(1);
  }, []);
  const handleFilterChange = useCallback((event: ChangeEvent<HTMLSelectElement>) => {
    setStateFilter(event.target.value as RadarState | "all");
    setPage(1);
  }, []);
  const previousPage = useCallback(() => setPage((current) => Math.max(1, current - 1)), []);
  const nextPage = useCallback(
    () => setPage((current) => Math.min(maximumPage, current + 1)),
    [maximumPage],
  );
  const runDiscovery = useCallback(() => {
    if (discoveryPending) {
      return;
    }
    setDiscoveryPending(true);
    setNotice("Queueing discovery over the private evidence inbox…");
    void requestJSON<RadarDiscoveryRun>(
      "/api/radar/discovery-runs",
      { idempotencyKey: `owner-radar-${crypto.randomUUID()}` },
      parseRadarDiscoveryRun,
    )
      .then((run) => {
        setRuns((current) => [run, ...current.filter((value) => value.id !== run.id)]);
        setNotice(
          "Discovery queued. Refresh after the maintenance worker completes the evidence pass.",
        );
      })
      .catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "Radar discovery could not be queued.");
      })
      .finally(() => setDiscoveryPending(false));
  }, [discoveryPending]);

  return (
    <section className="radar-workspace">
      <fieldset className="radar-state-grid">
        <legend className="sr-only">Radar state counts</legend>
        {radarStates.map((state) => (
          <article key={state}>
            <span>{state}</span>
            <strong>{stateCounts[state]}</strong>
          </article>
        ))}
      </fieldset>
      <div className="collection-toolbar source-toolbar radar-toolbar">
        <div className="source-filters">
          <label>
            Find candidate
            <span className="input-with-clear">
              <input
                onChange={handleQueryChange}
                placeholder="Package or incumbent"
                value={query}
              />
              <button disabled={query.length === 0} onClick={clearQuery} type="button">
                Clear
              </button>
            </span>
          </label>
          <label>
            State
            <select onChange={handleFilterChange} value={stateFilter}>
              <option value="all">All states</option>
              {radarStates.map((state) => (
                <option key={state} value={state}>
                  {state}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="radar-discovery-control">
          <p aria-live="polite">{notice}</p>
          <button disabled={discoveryPending} onClick={runDiscovery} type="button">
            {discoveryPending ? "Queueing…" : "Run evidence discovery"}
          </button>
        </div>
      </div>
      <p className="control-plane-count">
        Showing {visible.length} of {filtered.length} candidates · {runs.length} recorded runs
      </p>
      {visible.length === 0 ? (
        <div className="reading-empty-state radar-empty-state">
          <p className="eyebrow">No matching candidates</p>
          <h2>The evidence inbox has nothing ready for this view.</h2>
          <p>Run discovery after reviewed package evidence arrives, or clear the active filters.</p>
        </div>
      ) : (
        <div className="radar-candidate-list">
          {visible.map((candidate) => (
            <CandidateCard
              candidate={candidate}
              key={candidate.id}
              onChange={handleCandidateChange}
              timezone={timezone}
            />
          ))}
        </div>
      )}
      {maximumPage > 1 && (
        <nav aria-label="Radar candidate pages" className="source-pagination">
          <button disabled={page <= 1} onClick={previousPage} type="button">
            Previous
          </button>
          <span>
            Page {page} of {maximumPage}
          </span>
          <button disabled={page >= maximumPage} onClick={nextPage} type="button">
            Next
          </button>
        </nav>
      )}
    </section>
  );
};

export const RadarWorkspace = memo<RadarWorkspaceProps>(RadarWorkspaceComponent);
RadarWorkspace.displayName = "RadarWorkspace";
