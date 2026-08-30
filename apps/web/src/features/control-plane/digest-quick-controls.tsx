"use client";

import type { ScheduleActionResult, SchedulePreview } from "@relantern/domain";
import type { Route } from "next";
import Link from "next/link";
import { memo, useCallback, useState } from "react";
import { parseScheduleActionResult, parseSchedulePreview } from "@/features/control-plane/contract";

type DigestQuickControlsProps = Readonly<{
  scheduleID: string;
}>;

const decode = async <T,>(response: Response, parse: (value: unknown) => T): Promise<T> => {
  const value: unknown = await response.json();
  if (!response.ok) {
    const detail =
      typeof value === "object" && value !== null && "detail" in value
        ? String(value.detail)
        : "The digest control request failed.";
    throw new Error(detail);
  }
  return parse(value);
};

const DigestQuickControlsComponent = ({ scheduleID }: DigestQuickControlsProps) => {
  const [pending, setPending] = useState(false);
  const [notice, setNotice] = useState("");
  const [runKey, setRunKey] = useState(() => crypto.randomUUID());
  const preview = useCallback(() => {
    if (pending) return;
    setPending(true);
    void fetch(`/api/control-plane/settings/schedules/${scheduleID}/preview`, {
      headers: { accept: "application/json" },
      signal: AbortSignal.timeout(15_000),
    })
      .then((response) => decode<SchedulePreview>(response, parseSchedulePreview))
      .then((result) => {
        setNotice(
          `${result.candidateCount} eligible stories · ${result.externalDelivery ? "external channels configured" : "dashboard only"}`,
        );
      })
      .catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "Preview failed.");
      })
      .finally(() => setPending(false));
  }, [pending, scheduleID]);
  const runNow = useCallback(() => {
    if (pending) return;
    setPending(true);
    void fetch(`/api/control-plane/settings/schedules/${scheduleID}/actions`, {
      body: JSON.stringify({
        action: "run_now",
        deliver: false,
        idempotencyKey: runKey,
        reason: "Owner requested an immediate Today digest",
      }),
      headers: { accept: "application/json", "content-type": "application/json" },
      method: "POST",
      signal: AbortSignal.timeout(15_000),
    })
      .then((response) => decode<ScheduleActionResult>(response, parseScheduleActionResult))
      .then((result) => {
        setNotice(result.message);
        setRunKey(crypto.randomUUID());
      })
      .catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "Run-now preview failed.");
      })
      .finally(() => setPending(false));
  }, [pending, runKey, scheduleID]);

  return (
    <div className="header-actions digest-quick-controls">
      <button disabled={pending} onClick={preview} type="button">
        Preview digest
      </button>
      <button disabled={pending} onClick={runNow} type="button">
        Run digest now
      </button>
      <Link href={"/settings" as Route}>Schedule settings</Link>
      {notice !== "" && <span aria-live="polite">{notice}</span>}
    </div>
  );
};

export const DigestQuickControls = memo<DigestQuickControlsProps>(DigestQuickControlsComponent);
DigestQuickControls.displayName = "DigestQuickControls";
