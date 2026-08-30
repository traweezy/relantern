"use client";

import type { ImportCandidate, ImportPreview, ManualCapture } from "@relantern/domain";
import { useForm } from "@tanstack/react-form";
import type { ChangeEvent, FormEvent } from "react";
import { memo, useCallback, useMemo, useState } from "react";
import { z } from "zod";
import {
  parseImportCommit,
  parseImportPreview,
  parseManualCapture,
} from "@/features/discovery/contract";

const captureInputSchema = z.object({ url: z.url().max(4096) });
const opmlInputSchema = z.object({ opml: z.string().min(1).max(2_097_152) });

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
        : "The request failed.";
    throw new Error(detail);
  }
  return parse(decoded);
};

type CandidateRowProps = Readonly<{
  approved: boolean;
  candidate: ImportCandidate;
  onToggle: (candidateID: string) => void;
}>;

const CandidateRowComponent = ({ approved, candidate, onToggle }: CandidateRowProps) => {
  const handleChange = useCallback(() => {
    onToggle(candidate.id);
  }, [candidate.id, onToggle]);
  return (
    <li className="import-candidate">
      <label>
        <input
          checked={approved}
          disabled={!candidate.valid || candidate.duplicate}
          onChange={handleChange}
          type="checkbox"
        />
        <span>
          <strong>{candidate.name}</strong>
          <small>{candidate.url}</small>
        </span>
      </label>
      <div className="candidate-status">
        <span>{candidate.connector}</span>
        <span>{candidate.duplicate ? "Duplicate" : candidate.valid ? "Ready" : "Blocked"}</span>
      </div>
      <p>{candidate.explanation}</p>
    </li>
  );
};

const CandidateRow = memo<CandidateRowProps>(CandidateRowComponent);
CandidateRow.displayName = "CandidateRow";

const CapturePanelComponent = () => {
  const [capture, setCapture] = useState<ManualCapture | null>(null);
  const [captureKey, setCaptureKey] = useState(() => crypto.randomUUID());
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [approvedIDs, setApprovedIDs] = useState<ReadonlySet<string>>(() => new Set());
  const [notice, setNotice] = useState("Add a single article or stage a source list for review.");

  const captureForm = useForm({
    defaultValues: { url: "" },
    validators: { onSubmit: captureInputSchema },
    onSubmit: async ({ value }) => {
      const result = await requestJSON(
        "/api/discovery/import-url",
        { idempotencyKey: captureKey, url: value.url },
        parseManualCapture,
      );
      setCapture(result);
      setCaptureKey(crypto.randomUUID());
      setNotice("Capture queued. It will enter Inbox only after policy and evidence checks pass.");
      captureForm.reset();
    },
  });

  const opmlForm = useForm({
    defaultValues: { opml: "" },
    validators: { onSubmit: opmlInputSchema },
    onSubmit: async ({ value }) => {
      const result = await requestJSON("/api/discovery/opml/preview", value, parseImportPreview);
      setPreview(result);
      setApprovedIDs(new Set());
      setNotice("Preview ready. Approve each valid, non-duplicate source explicitly.");
    },
  });

  const handleCaptureSubmit = useCallback(
    (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void captureForm.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The URL could not be queued.");
      });
    },
    [captureForm],
  );

  const handleOPMLSubmit = useCallback(
    (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void opmlForm.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The OPML preview failed.");
      });
    },
    [opmlForm],
  );

  const handleOPMLFile = useCallback(
    (event: ChangeEvent<HTMLInputElement>) => {
      const file = event.target.files?.[0];
      if (file === undefined) {
        return;
      }
      if (file.size > 2_097_152) {
        setNotice("OPML files are limited to 2 MiB.");
        event.target.value = "";
        return;
      }
      void file
        .text()
        .then((value) => opmlForm.setFieldValue("opml", value))
        .catch(() => setNotice("The OPML file could not be read."));
    },
    [opmlForm],
  );

  const clearCapture = useCallback(() => {
    captureForm.reset();
    setCapture(null);
    setCaptureKey(crypto.randomUUID());
  }, [captureForm]);

  const clearOPML = useCallback(() => {
    opmlForm.reset();
    setPreview(null);
    setApprovedIDs(new Set());
  }, [opmlForm]);

  const toggleApproved = useCallback((candidateID: string) => {
    setApprovedIDs((current) => {
      const next = new Set(current);
      if (next.has(candidateID)) {
        next.delete(candidateID);
      } else {
        next.add(candidateID);
      }
      return next;
    });
  }, []);

  const commitPreview = useCallback(() => {
    if (preview === null || approvedIDs.size === 0) {
      setNotice("Select at least one valid source before importing.");
      return;
    }
    setNotice("Creating disabled, pending sources…");
    void requestJSON(
      "/api/discovery/opml/commit",
      { approvedIds: [...approvedIDs], previewId: preview.id },
      parseImportCommit,
    )
      .then((result) => {
        setNotice(
          `${result.importedCount} source${result.importedCount === 1 ? "" : "s"} staged for validation. Nothing was enabled automatically.`,
        );
        setPreview(null);
        setApprovedIDs(new Set());
        opmlForm.reset();
      })
      .catch((error: unknown) => {
        setNotice(
          error instanceof Error ? error.message : "The approved sources were not imported.",
        );
      });
  }, [approvedIDs, opmlForm, preview]);

  const readyCount = useMemo(
    () =>
      preview?.candidates.filter((candidate) => candidate.valid && !candidate.duplicate).length ??
      0,
    [preview],
  );

  return (
    <section aria-labelledby="capture-title" className="capture-panel">
      <div className="capture-panel-heading">
        <div>
          <p className="eyebrow">Owner capture · policy checked</p>
          <h2 id="capture-title">Bring signal into the pipeline.</h2>
        </div>
        <p aria-live="polite">{notice}</p>
      </div>

      <div className="capture-grid">
        <form className="capture-card" onSubmit={handleCaptureSubmit}>
          <div>
            <span className="capture-step">01</span>
            <h3>Add to Inbox</h3>
            <p>
              Fetch one HTTPS article through the same SSRF, robots, dedupe, and provenance path.
            </p>
          </div>
          <captureForm.Field name="url">
            {(field) => (
              <label>
                Article URL
                <input
                  aria-describedby="capture-url-help"
                  aria-invalid={field.state.meta.errors.length > 0}
                  inputMode="url"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  placeholder="https://example.com/release-notes"
                  type="url"
                  value={field.state.value}
                />
                <small id="capture-url-help">
                  Queued first; publication remains evidence-gated.
                </small>
              </label>
            )}
          </captureForm.Field>
          <div className="capture-file-row">
            <captureForm.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
              {([canSubmit, isSubmitting]) => (
                <button disabled={!canSubmit || isSubmitting} type="submit">
                  {isSubmitting ? "Queueing…" : "Queue capture"}
                </button>
              )}
            </captureForm.Subscribe>
            <button className="capture-clear" onClick={clearCapture} type="button">
              Clear
            </button>
          </div>
          {capture !== null && (
            <output className="capture-receipt">
              Capture {capture.id.slice(0, 8)} · {capture.state}
            </output>
          )}
        </form>

        <form className="capture-card" onSubmit={handleOPMLSubmit}>
          <div>
            <span className="capture-step">02</span>
            <h3>Preview OPML</h3>
            <p>Inspect connector support and duplicates before any source record is created.</p>
          </div>
          <opmlForm.Field name="opml">
            {(field) => (
              <label>
                OPML document
                <textarea
                  aria-describedby="opml-help"
                  aria-invalid={field.state.meta.errors.length > 0}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  placeholder={'<?xml version="1.0"?><opml version="2.0">…'}
                  rows={5}
                  value={field.state.value}
                />
                <small id="opml-help">Maximum 500 feeds and 2 MiB. HTTPS feeds only.</small>
              </label>
            )}
          </opmlForm.Field>
          <div className="capture-file-row">
            <label className="file-button">
              Choose OPML file
              <input
                accept=".opml,.xml,text/xml,application/xml"
                onChange={handleOPMLFile}
                type="file"
              />
            </label>
            <opmlForm.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
              {([canSubmit, isSubmitting]) => (
                <button disabled={!canSubmit || isSubmitting} type="submit">
                  {isSubmitting ? "Inspecting…" : "Create preview"}
                </button>
              )}
            </opmlForm.Subscribe>
            <button className="capture-clear" onClick={clearOPML} type="button">
              Clear
            </button>
          </div>
        </form>
      </div>

      {preview !== null && (
        <section aria-labelledby="import-preview-title" className="import-preview">
          <div className="import-preview-heading">
            <div>
              <p className="eyebrow">Explicit approval</p>
              <h3 id="import-preview-title">{readyCount} sources ready for review</h3>
            </div>
            <button disabled={approvedIDs.size === 0} onClick={commitPreview} type="button">
              Import {approvedIDs.size} as pending
            </button>
          </div>
          <ul>
            {preview.candidates.map((candidate) => (
              <CandidateRow
                approved={approvedIDs.has(candidate.id)}
                candidate={candidate}
                key={candidate.id}
                onToggle={toggleApproved}
              />
            ))}
          </ul>
        </section>
      )}

      <section aria-label="Portable exports" className="export-strip">
        <div>
          <p className="eyebrow">Your data stays portable</p>
          <p>Export source subscriptions or private story metadata at any time.</p>
        </div>
        <nav aria-label="Export formats">
          <a href="/api/discovery/exports/opml">OPML</a>
          <a href="/api/discovery/exports/metadata?format=json">JSON</a>
          <a href="/api/discovery/exports/metadata?format=csv">CSV</a>
        </nav>
      </section>
    </section>
  );
};

export const CapturePanel = memo(CapturePanelComponent);
CapturePanel.displayName = "CapturePanel";
