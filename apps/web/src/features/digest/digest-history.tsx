"use client";

import type { DigestRecord } from "@relantern/domain";
import type { Route } from "next";
import Link from "next/link";
import { memo, useCallback, useState } from "react";
import { parseDigestRecord } from "./contract";

type DigestHistoryProps = Readonly<{
  initialDigests: readonly DigestRecord[];
  timezone: string;
}>;

type DigestCardProps = Readonly<{
  initialDigest: DigestRecord;
  timezone: string;
}>;

const formatTime = (value: string, timezone: string): string =>
  new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone,
  }).format(new Date(value));

const parseResponse = async (response: Response): Promise<DigestRecord> => {
  const decoded: unknown = await response.json();
  if (!response.ok) {
    const detail =
      typeof decoded === "object" && decoded !== null && "detail" in decoded
        ? String(decoded.detail)
        : "The delivery retry failed.";
    throw new Error(detail);
  }
  return parseDigestRecord(decoded);
};

const DigestCardComponent = ({ initialDigest, timezone }: DigestCardProps) => {
  const [current, setCurrent] = useState(initialDigest);
  const [pending, setPending] = useState(false);
  const [notice, setNotice] = useState("");
  const retry = useCallback(() => {
    if (pending || current.state !== "failed") return;
    setPending(true);
    setNotice("");
    void fetch(`/api/digests/${encodeURIComponent(current.id)}/retry`, {
      headers: { accept: "application/json" },
      method: "POST",
      signal: AbortSignal.timeout(15_000),
    })
      .then(parseResponse)
      .then((updated) => {
        setCurrent(updated);
        setNotice("Retry queued with the original payload and provider key.");
      })
      .catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The delivery retry failed.");
      })
      .finally(() => setPending(false));
  }, [current.id, current.state, pending]);

  return (
    <article className="digest-ledger-card">
      <header>
        <div>
          <p className="eyebrow">{current.channel} payload</p>
          <h3>{current.localDate}</h3>
        </div>
        <span className={`digest-state digest-state-${current.state}`}>{current.state}</span>
      </header>
      <p>{current.executiveSummary}</p>
      <dl className="digest-ledger-facts">
        <div>
          <dt>Window</dt>
          <dd>
            {formatTime(current.windowStart, timezone)} – {formatTime(current.windowEnd, timezone)}
          </dd>
        </div>
        <div>
          <dt>Items</dt>
          <dd>{current.items.length}</dd>
        </div>
        <div>
          <dt>Attempts</dt>
          <dd>{current.attemptCount}</dd>
        </div>
      </dl>
      {current.items.length > 0 && (
        <details className="digest-ledger-items">
          <summary>Selected evidence · {current.items.length}</summary>
          <ol>
            {current.items.map((item) => (
              <li key={`${item.type}:${item.id}`}>
                <div>
                  <span>{item.category.replace("_", " ")}</span>
                  <strong>{item.headline}</strong>
                </div>
                <p>{item.reason}</p>
                <nav aria-label={`${item.headline} evidence links`}>
                  <Link href={item.appPath as Route}>Open in Relantern</Link>
                  <a href={item.sourceUrl} rel="noreferrer" target="_blank">
                    Primary source
                  </a>
                </nav>
              </li>
            ))}
          </ol>
        </details>
      )}
      <footer>
        <code title={current.providerIdempotencyKey}>
          {current.providerIdempotencyKey.slice(0, 30)}…
        </code>
        {current.state === "failed" && (
          <button disabled={pending} onClick={retry} type="button">
            {pending ? "Queuing retry…" : "Retry delivery"}
          </button>
        )}
      </footer>
      {notice !== "" && (
        <p aria-live="polite" className="digest-ledger-notice">
          {notice}
        </p>
      )}
    </article>
  );
};

const DigestCard = memo<DigestCardProps>(DigestCardComponent);
DigestCard.displayName = "DigestCard";

const DigestHistoryComponent = ({ initialDigests, timezone }: DigestHistoryProps) => (
  <section aria-labelledby="delivery-ledger-title" className="digest-ledger-section">
    <div className="collection-toolbar">
      <div>
        <p className="eyebrow">Immutable delivery ledger</p>
        <h2 id="delivery-ledger-title">Recent digest payloads</h2>
      </div>
      <p>Retries keep the same content hash and provider idempotency key.</p>
    </div>
    {initialDigests.length === 0 ? (
      <div className="digest-ledger-empty">
        <strong>No finalized digests yet</strong>
        <span>The next completed dashboard brief will establish the first immutable window.</span>
      </div>
    ) : (
      <div className="digest-ledger-grid">
        {initialDigests.slice(0, 6).map((current) => (
          <DigestCard initialDigest={current} key={current.id} timezone={timezone} />
        ))}
      </div>
    )}
  </section>
);

export const DigestHistory = memo<DigestHistoryProps>(DigestHistoryComponent);
DigestHistory.displayName = "DigestHistory";
