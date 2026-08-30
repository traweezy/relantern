"use client";

import type { ManagedSource, SourcePreference } from "@relantern/domain";
import { useForm } from "@tanstack/react-form";
import type { FormEvent } from "react";
import { memo, useCallback, useMemo, useState } from "react";
import { sourcePreferenceCommandSchema } from "@/features/control-plane/commands";
import { parseManagedSource, parseSourcePreference } from "@/features/control-plane/contract";

type SourceRegistryProps = Readonly<{
  initialSources: readonly ManagedSource[];
  timezone: string;
}>;

type SourceCardProps = Readonly<{
  onChange: (source: ManagedSource) => void;
  source: ManagedSource;
  timezone: string;
}>;

const sourcesPerPage = 5;

const requestJSON = async <T,>(
  path: string,
  body: unknown,
  method: "POST" | "PUT",
  parse: (value: unknown) => T,
): Promise<T> => {
  const response = await fetch(path, {
    body: JSON.stringify(body),
    headers: { accept: "application/json", "content-type": "application/json" },
    method,
    signal: AbortSignal.timeout(15_000),
  });
  const decoded: unknown = await response.json();
  if (!response.ok) {
    const detail =
      typeof decoded === "object" && decoded !== null && "detail" in decoded
        ? String(decoded.detail)
        : "The source control request failed.";
    throw new Error(detail);
  }
  return parse(decoded);
};

const formatTime = (value: string | undefined, timezone: string): string => {
  if (value === undefined) {
    return "Never";
  }
  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone,
  }).format(new Date(value));
};

const formatCadence = (seconds: number): string => {
  if (seconds % 86_400 === 0) {
    return `${seconds / 86_400}d`;
  }
  if (seconds % 3_600 === 0) {
    return `${seconds / 3_600}h`;
  }
  return `${Math.round(seconds / 60)}m`;
};

const SourceCardComponent = ({ onChange, source, timezone }: SourceCardProps) => {
  const [notice, setNotice] = useState("Preferences and polling controls are independent.");
  const [reason, setReason] = useState("Owner source-health review");
  const [actionPending, setActionPending] = useState(false);
  const preferenceForm = useForm({
    defaultValues: {
      excludeFromDigest: source.preference.excludeFromDigest,
      expectedVersion: source.preference.version,
      muted: source.preference.muted,
      relevanceAdjustment: source.preference.relevanceAdjustment,
    },
    validators: { onSubmit: sourcePreferenceCommandSchema },
    onSubmit: async ({ value }) => {
      const preference = await requestJSON<SourcePreference>(
        `/api/control-plane/sources/${encodeURIComponent(source.id)}/preference`,
        value,
        "PUT",
        parseSourcePreference,
      );
      onChange({ ...source, preference });
      preferenceForm.setFieldValue("expectedVersion", preference.version);
      setNotice(`Preference version ${preference.version} saved.`);
    },
  });

  const handlePreferenceSubmit = useCallback(
    (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void preferenceForm.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The source preference was not saved.");
      });
    },
    [preferenceForm],
  );

  const runAction = useCallback(
    (action: "approve" | "pause" | "reject" | "resume" | "test") => {
      if (actionPending || reason.trim().length < 3) {
        setNotice("Add a short audit reason before running a source action.");
        return;
      }
      setActionPending(true);
      setNotice(`${action.replace("_", " ")} in progress…`);
      void requestJSON(
        `/api/control-plane/sources/${encodeURIComponent(source.id)}/actions`,
        { action, reason: reason.trim() },
        "POST",
        parseManagedSource,
      )
        .then((updated) => {
          onChange(updated);
          setNotice(
            action === "test"
              ? "Deterministic configuration checks completed without network access."
              : `Source ${action.replace("_", " ")} recorded with an audit event.`,
          );
        })
        .catch((error: unknown) => {
          setNotice(error instanceof Error ? error.message : "The source action failed.");
        })
        .finally(() => setActionPending(false));
    },
    [actionPending, onChange, reason, source.id],
  );

  const handleTest = useCallback(() => runAction("test"), [runAction]);
  const handleApprove = useCallback(() => runAction("approve"), [runAction]);
  const handleReject = useCallback(() => runAction("reject"), [runAction]);
  const handlePause = useCallback(() => runAction("pause"), [runAction]);
  const handleResume = useCallback(() => runAction("resume"), [runAction]);
  const handleReasonChange = useCallback((event: React.ChangeEvent<HTMLInputElement>) => {
    setReason(event.target.value);
  }, []);
  const clearReason = useCallback(() => setReason(""), []);

  return (
    <article className="source-control-card">
      <header>
        <div>
          <div className="story-badges">
            <span className="source-tier">{source.trustTier}</span>
            <span className={`source-health source-health-${source.errorBudgetState}`}>
              {source.errorBudgetState}
            </span>
            <span className="signal-badge">{source.origin}</span>
          </div>
          <h2>{source.name}</h2>
          <p>{source.topics.join(" · ")}</p>
        </div>
        <div className="source-state-stack">
          <strong>{source.pollingEnabled ? "Polling allowed" : "Polling stopped"}</strong>
          <span>{source.validationState}</span>
        </div>
      </header>

      <dl className="source-metric-grid">
        <div>
          <dt>Content</dt>
          <dd>{source.contentCount}</dd>
        </div>
        <div>
          <dt>Duplicate rate</dt>
          <dd>{Math.round(source.duplicateRate * 100)}%</dd>
        </div>
        <div>
          <dt>24h success</dt>
          <dd>{source.successCount24h}</dd>
        </div>
        <div>
          <dt>24h failure</dt>
          <dd>{source.failureCount24h}</dd>
        </div>
      </dl>

      <div className="source-endpoint-wrap">
        <table className="source-endpoint-table">
          <caption>Endpoint health for {source.name}</caption>
          <thead>
            <tr>
              <th scope="col">Connector</th>
              <th scope="col">Cadence</th>
              <th scope="col">Last success</th>
              <th scope="col">HTTP / error</th>
            </tr>
          </thead>
          <tbody>
            {source.endpoints.map((endpoint) => (
              <tr key={endpoint.id}>
                <th scope="row">
                  {endpoint.connector}
                  <small>{endpoint.priority}</small>
                </th>
                <td>{formatCadence(endpoint.pollIntervalSeconds)}</td>
                <td>{formatTime(endpoint.lastSuccessAt, timezone)}</td>
                <td>
                  {endpoint.latestStatusCode ?? "—"}
                  {endpoint.latestErrorCode === undefined || endpoint.latestErrorCode === ""
                    ? ""
                    : ` · ${endpoint.latestErrorCode}`}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <form className="source-preference-form" onSubmit={handlePreferenceSubmit}>
        <div>
          <p className="eyebrow">Owner preference</p>
          <p>Mute ranking or exclude delivery without stopping collection.</p>
        </div>
        <preferenceForm.Field name="muted">
          {(field) => (
            <label className="control-check">
              <input
                checked={field.state.value}
                onChange={(event) => field.handleChange(event.target.checked)}
                type="checkbox"
              />
              Mute ranking
            </label>
          )}
        </preferenceForm.Field>
        <preferenceForm.Field name="excludeFromDigest">
          {(field) => (
            <label className="control-check">
              <input
                checked={field.state.value}
                onChange={(event) => field.handleChange(event.target.checked)}
                type="checkbox"
              />
              Exclude from digest
            </label>
          )}
        </preferenceForm.Field>
        <preferenceForm.Field name="relevanceAdjustment">
          {(field) => (
            <label>
              Relevance adjustment
              <input
                max="1"
                min="-1"
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.valueAsNumber)}
                step="0.05"
                type="number"
                value={field.state.value}
              />
            </label>
          )}
        </preferenceForm.Field>
        <preferenceForm.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
          {([canSubmit, isSubmitting]) => (
            <button disabled={!canSubmit || isSubmitting} type="submit">
              {isSubmitting ? "Saving…" : "Save preference"}
            </button>
          )}
        </preferenceForm.Subscribe>
      </form>

      <section aria-label={`${source.name} operator actions`} className="source-action-panel">
        <label>
          Audit reason
          <span className="input-with-clear">
            <input maxLength={1000} onChange={handleReasonChange} type="text" value={reason} />
            <button aria-label="Clear audit reason" onClick={clearReason} type="button">
              Clear
            </button>
          </span>
        </label>
        <div className="source-action-row">
          <button disabled={actionPending} onClick={handleTest} type="button">
            Test config
          </button>
          {source.pollingEnabled ? (
            <button disabled={actionPending} onClick={handlePause} type="button">
              Pause polling
            </button>
          ) : (
            <button
              disabled={
                actionPending ||
                source.validationState === "pending" ||
                source.validationState === "rejected"
              }
              onClick={handleResume}
              type="button"
            >
              Resume polling
            </button>
          )}
          {source.origin === "owner" && source.validationState === "pending" && (
            <>
              <button
                disabled={actionPending || source.latestValidation?.state !== "passed"}
                onClick={handleApprove}
                type="button"
              >
                Approve source
              </button>
              <button
                className="danger-button"
                disabled={actionPending}
                onClick={handleReject}
                type="button"
              >
                Reject
              </button>
            </>
          )}
          <a href={source.homepageUrl} rel="noreferrer" target="_blank">
            Open source
          </a>
        </div>
        {source.latestValidation !== undefined && (
          <p className="source-validation-copy">
            Last test {formatTime(source.latestValidation.completedAt, timezone)} ·{" "}
            {source.latestValidation.state} · {source.latestValidation.explanation}
          </p>
        )}
        <p aria-live="polite" className="control-notice">
          {notice}
        </p>
      </section>
    </article>
  );
};

const SourceCard = memo<SourceCardProps>(SourceCardComponent);
SourceCard.displayName = "SourceCard";

const SourceRegistryComponent = ({ initialSources, timezone }: SourceRegistryProps) => {
  const [sources, setSources] = useState<readonly ManagedSource[]>(initialSources);
  const [query, setQuery] = useState("");
  const [tier, setTier] = useState("all");
  const [pageIndex, setPageIndex] = useState(0);
  const updateSource = useCallback((updated: ManagedSource) => {
    setSources((current) => current.map((source) => (source.id === updated.id ? updated : source)));
  }, []);
  const visibleSources = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    return sources.filter(
      (source) =>
        (tier === "all" || source.trustTier === tier) &&
        (normalized === "" ||
          source.name.toLowerCase().includes(normalized) ||
          source.id.toLowerCase().includes(normalized) ||
          source.topics.some((topic) => topic.toLowerCase().includes(normalized))),
    );
  }, [query, sources, tier]);
  const pageCount = Math.max(1, Math.ceil(visibleSources.length / sourcesPerPage));
  const activePageIndex = Math.min(pageIndex, pageCount - 1);
  const pageStart = activePageIndex * sourcesPerPage;
  const pagedSources = useMemo(
    () => visibleSources.slice(pageStart, pageStart + sourcesPerPage),
    [pageStart, visibleSources],
  );
  const handleQuery = useCallback((event: React.ChangeEvent<HTMLInputElement>) => {
    setQuery(event.target.value);
    setPageIndex(0);
  }, []);
  const handleTier = useCallback((event: React.ChangeEvent<HTMLSelectElement>) => {
    setTier(event.target.value);
    setPageIndex(0);
  }, []);
  const clearQuery = useCallback(() => {
    setQuery("");
    setPageIndex(0);
  }, []);
  const showPreviousPage = useCallback(() => {
    setPageIndex((current) => Math.max(0, current - 1));
  }, []);
  const showNextPage = useCallback(() => {
    setPageIndex((current) => Math.min(pageCount - 1, current + 1));
  }, [pageCount]);

  return (
    <section aria-labelledby="source-registry-title" className="control-plane-section">
      <div className="collection-toolbar source-toolbar">
        <div>
          <p className="eyebrow">Collection control</p>
          <h2 id="source-registry-title">Registry and health</h2>
        </div>
        <div className="source-filters">
          <label>
            Search sources
            <span className="input-with-clear">
              <input onChange={handleQuery} type="search" value={query} />
              <button onClick={clearQuery} type="button">
                Clear
              </button>
            </span>
          </label>
          <label>
            Trust tier
            <select onChange={handleTier} value={tier}>
              <option value="all">All tiers</option>
              <option value="T0">T0</option>
              <option value="T1">T1</option>
              <option value="T2">T2</option>
              <option value="T3">T3</option>
            </select>
          </label>
        </div>
      </div>
      <p className="control-plane-count">
        {visibleSources.length === 0
          ? `No sources match the active filters (${sources.length} total). `
          : `Showing ${pageStart + 1}–${pageStart + pagedSources.length} of ${visibleSources.length} matching sources (${sources.length} total). `}
        A healthy 24-hour error budget permits at most 5% failed polls.
      </p>
      <div className="source-control-list">
        {pagedSources.map((source) => (
          <SourceCard key={source.id} onChange={updateSource} source={source} timezone={timezone} />
        ))}
      </div>
      {visibleSources.length > sourcesPerPage && (
        <nav aria-label="Source registry pages" className="source-pagination">
          <button disabled={activePageIndex === 0} onClick={showPreviousPage} type="button">
            Previous
          </button>
          <span aria-live="polite">
            Page {activePageIndex + 1} of {pageCount}
          </span>
          <button disabled={activePageIndex === pageCount - 1} onClick={showNextPage} type="button">
            Next
          </button>
        </nav>
      )}
    </section>
  );
};

export const SourceRegistry = memo<SourceRegistryProps>(SourceRegistryComponent);
SourceRegistry.displayName = "SourceRegistry";
