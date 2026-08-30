"use client";

import type {
  ReadingAction,
  ReadingMutation,
  StoryAnnotation,
  StoryDetail,
  StoryFeedbackType,
  StoryReadingState,
  StoryTag,
} from "@relantern/domain";
import { useForm } from "@tanstack/react-form";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  customSnoozeFormSchema,
  dismissalFormSchema,
  highlightBodySchema,
  noteBodySchema,
  tagInputSchema,
} from "./commands";
import {
  parseAnnotationResponse,
  parseFeedbackResponse,
  parseReadingMutation,
  parseTagResponse,
} from "./contract";
import { customSnoozeTime, snoozePresetTime } from "./snooze-time";
import { buildStoryMarkdown } from "./story-export";

type StoryWorkspaceProps = Readonly<{
  initialAnnotations: readonly StoryAnnotation[];
  initialState: StoryReadingState;
  initialTags: readonly StoryTag[];
  story: StoryDetail;
  timezone: string;
}>;

type UndoState = Readonly<{
  deadline: string;
  mutationId: string;
}>;

type DismissalReason =
  | "already_known"
  | "duplicate"
  | "irrelevant_topic"
  | "low_quality"
  | "too_promotional";

type AnnotationRowProps = Readonly<{
  annotation: StoryAnnotation;
  onDelete: (annotationId: string) => void;
}>;

type StoryTagToggleProps = Readonly<{
  active: boolean;
  onToggle: (tagId: string) => void;
  tag: StoryTag;
}>;

type FeedbackButtonProps = Readonly<{
  disabled: boolean;
  label: string;
  onFeedback: (type: StoryFeedbackType) => void;
  type: StoryFeedbackType;
}>;

const requestJSON = async <T,>(
  path: string,
  method: string,
  body: unknown,
  parse: (value: unknown) => T,
): Promise<T> => {
  const response = await fetch(path, {
    body: JSON.stringify(body),
    headers: { accept: "application/json", "content-type": "application/json" },
    method,
    signal: AbortSignal.timeout(7_000),
  });
  const decoded: unknown = await response.json();
  if (!response.ok) {
    const detail =
      typeof decoded === "object" && decoded !== null && "detail" in decoded
        ? String(decoded.detail)
        : "The private command failed.";
    throw new Error(detail);
  }
  return parse(decoded);
};

const digestHex = async (value: string): Promise<string> => {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, "0")).join("");
};

const selectedSourceSpan = (): Readonly<{ end: number; quote: string; start: number }> | null => {
  const surface = document.getElementById("normalized-source-content");
  const selection = window.getSelection();
  if (
    surface === null ||
    selection === null ||
    selection.rangeCount !== 1 ||
    selection.isCollapsed
  ) {
    return null;
  }
  const range = selection.getRangeAt(0);
  if (!surface.contains(range.startContainer) || !surface.contains(range.endContainer)) {
    return null;
  }
  const prefix = range.cloneRange();
  prefix.selectNodeContents(surface);
  prefix.setEnd(range.startContainer, range.startOffset);
  const quote = range.toString();
  const start = [...prefix.toString()].length;
  return quote.trim() === "" ? null : { end: start + [...quote].length, quote, start };
};

const AnnotationRowComponent = ({ annotation, onDelete }: AnnotationRowProps) => {
  const handleDelete = useCallback(() => onDelete(annotation.id), [annotation.id, onDelete]);
  const label = annotation.type === "document_note" ? "Note" : "Highlight";
  return (
    <li
      className={annotation.orphaned ? "annotation-row annotation-row-orphaned" : "annotation-row"}
    >
      <div>
        <strong>{label}</strong>
        {annotation.orphaned && <span>Source changed · original revision retained</span>}
      </div>
      {annotation.body !== "" && <p>{annotation.body}</p>}
      {annotation.startOffset !== null && annotation.endOffset !== null && (
        <small>
          Characters {annotation.startOffset}–{annotation.endOffset} · revision{" "}
          {annotation.revisionId.slice(0, 8)}
        </small>
      )}
      <button onClick={handleDelete} type="button">
        Delete
      </button>
    </li>
  );
};

const AnnotationRow = memo<AnnotationRowProps>(AnnotationRowComponent);
AnnotationRow.displayName = "AnnotationRow";

const StoryTagToggleComponent = ({ active, onToggle, tag }: StoryTagToggleProps) => {
  const handleToggle = useCallback(() => onToggle(tag.id), [onToggle, tag.id]);
  return (
    <button
      aria-pressed={active}
      className={`tag-chip tag-${tag.colorToken}${active ? " tag-chip-active" : ""}`}
      onClick={handleToggle}
      type="button"
    >
      {tag.name}
    </button>
  );
};

const StoryTagToggle = memo<StoryTagToggleProps>(StoryTagToggleComponent);
StoryTagToggle.displayName = "StoryTagToggle";

const FeedbackButtonComponent = ({ disabled, label, onFeedback, type }: FeedbackButtonProps) => {
  const handleClick = useCallback(() => onFeedback(type), [onFeedback, type]);
  return (
    <button disabled={disabled} onClick={handleClick} type="button">
      {label}
    </button>
  );
};

const FeedbackButton = memo<FeedbackButtonProps>(FeedbackButtonComponent);
FeedbackButton.displayName = "FeedbackButton";

const StoryWorkspaceComponent = ({
  initialAnnotations,
  initialState,
  initialTags,
  story,
  timezone,
}: StoryWorkspaceProps) => {
  const revisionId = story.revisionId;
  const storyId = story.id;
  const [state, setState] = useState(initialState);
  const [annotations, setAnnotations] = useState(initialAnnotations);
  const [tags, setTags] = useState(initialTags);
  const [pending, setPending] = useState(false);
  const [feedbackPending, setFeedbackPending] = useState(false);
  const [notice, setNotice] = useState("");
  const [undo, setUndo] = useState<UndoState | null>(null);
  const autoReadSent = useRef(false);
  const pendingRef = useRef(false);
  const progressRestored = useRef(false);
  const stateRef = useRef(initialState);

  const mutate = useCallback(
    async (
      action: ReadingAction,
      options: Readonly<{
        dismissedReason?: DismissalReason;
        lastParagraphId?: string;
        readingProgress?: number;
        snoozedUntil?: string;
        tagId?: string;
      }> = {},
      quiet = false,
    ): Promise<boolean> => {
      if (pendingRef.current) {
        return false;
      }
      pendingRef.current = true;
      setPending(true);
      if (!quiet) {
        setNotice("Saving command…");
      }
      try {
        const mutation = await requestJSON(
          `/api/reading-state/stories/${encodeURIComponent(storyId)}/state`,
          "PATCH",
          {
            action,
            idempotencyKey: crypto.randomUUID(),
            version: stateRef.current.version,
            ...(options.dismissedReason === undefined
              ? {}
              : { dismissedReason: options.dismissedReason }),
            ...(options.lastParagraphId === undefined
              ? {}
              : { lastParagraphId: options.lastParagraphId }),
            ...(options.readingProgress === undefined
              ? {}
              : { readingProgress: options.readingProgress }),
            ...(options.snoozedUntil === undefined ? {} : { snoozedUntil: options.snoozedUntil }),
            ...(options.tagId === undefined ? {} : { tagId: options.tagId }),
          },
          parseReadingMutation,
        );
        stateRef.current = mutation.state;
        setState(mutation.state);
        if (!quiet) {
          setUndo({ deadline: mutation.undoDeadline, mutationId: mutation.mutationId });
          setNotice("Saved. Undo is available for ten seconds.");
        }
        return true;
      } catch (error: unknown) {
        if (!quiet) {
          setNotice(error instanceof Error ? error.message : "The command failed.");
        }
        return false;
      } finally {
        pendingRef.current = false;
        setPending(false);
      }
    },
    [storyId],
  );

  useEffect(() => {
    if (state.isRead || autoReadSent.current || pending) {
      return;
    }
    const timer = window.setTimeout(() => {
      void mutate("mark_read").then((saved) => {
        autoReadSent.current = saved;
      });
    }, 2_000);
    return () => window.clearTimeout(timer);
  }, [mutate, pending, state.isRead]);

  useEffect(() => {
    if (undo === null) {
      return;
    }
    const remaining = Math.max(0, Date.parse(undo.deadline) - Date.now());
    const timer = window.setTimeout(() => setUndo(null), remaining);
    return () => window.clearTimeout(timer);
  }, [undo]);

  useEffect(() => {
    if (progressRestored.current) {
      return;
    }
    const surface = document.getElementById("normalized-source-content");
    if (surface === null) {
      return;
    }
    progressRestored.current = true;
    const maximumScroll = surface.scrollHeight - surface.clientHeight;
    if (stateRef.current.lastParagraphId !== null) {
      const paragraph = document.getElementById(stateRef.current.lastParagraphId);
      if (paragraph !== null && surface.contains(paragraph)) {
        surface.scrollTop = Math.max(0, paragraph.offsetTop - surface.offsetTop);
        return;
      }
    }
    if (maximumScroll > 0 && stateRef.current.readingProgress > 0) {
      surface.scrollTop = maximumScroll * stateRef.current.readingProgress;
    }
  }, []);

  useEffect(() => {
    if (new URLSearchParams(window.location.search).get("note") === "1") {
      document.getElementById("story-document-note")?.focus();
    }
  }, []);

  useEffect(() => {
    const surface = document.getElementById("normalized-source-content");
    if (surface === null) {
      return;
    }
    let saveTimer: number | undefined;
    const saveProgress = () => {
      if (undo !== null || pendingRef.current) {
        return;
      }
      const maximumScroll = surface.scrollHeight - surface.clientHeight;
      const readingProgress =
        maximumScroll <= 0 ? 0 : Math.min(1, Math.max(0, surface.scrollTop / maximumScroll));
      const surfaceTop = surface.getBoundingClientRect().top + 24;
      let lastParagraphId: string | undefined;
      for (const paragraph of surface.querySelectorAll<HTMLElement>("[data-reader-paragraph]")) {
        if (paragraph.getBoundingClientRect().top <= surfaceTop) {
          lastParagraphId = paragraph.id;
        } else {
          break;
        }
      }
      const current = stateRef.current;
      if (
        Math.abs(current.readingProgress - readingProgress) < 0.01 &&
        (lastParagraphId === undefined || current.lastParagraphId === lastParagraphId)
      ) {
        return;
      }
      void mutate(
        "update_progress",
        {
          ...(lastParagraphId === undefined ? {} : { lastParagraphId }),
          readingProgress: Number(readingProgress.toFixed(5)),
        },
        true,
      );
    };
    const scheduleSave = () => {
      if (saveTimer !== undefined) {
        window.clearTimeout(saveTimer);
      }
      saveTimer = window.setTimeout(saveProgress, 1_200);
    };
    surface.addEventListener("scroll", scheduleSave, { passive: true });
    return () => {
      surface.removeEventListener("scroll", scheduleSave);
      if (saveTimer !== undefined) {
        window.clearTimeout(saveTimer);
      }
    };
  }, [mutate, undo]);

  const handleUndo = useCallback(() => {
    if (undo === null || pending) {
      return;
    }
    setPending(true);
    void requestJSON<ReadingMutation>(
      `/api/reading-state/stories/${encodeURIComponent(storyId)}/undo`,
      "POST",
      { mutationId: undo.mutationId },
      parseReadingMutation,
    )
      .then((mutation) => {
        stateRef.current = mutation.state;
        setState(mutation.state);
        setUndo(null);
        setNotice("Prior state restored.");
      })
      .catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "Undo failed.");
      })
      .finally(() => setPending(false));
  }, [pending, storyId, undo]);

  const handleRead = useCallback(
    () => void mutate(state.isRead ? "mark_unread" : "mark_read"),
    [mutate, state.isRead],
  );
  const handleLater = useCallback(
    () => void mutate(state.location === "later" ? "move_inbox" : "move_later"),
    [mutate, state.location],
  );
  const handleStar = useCallback(
    () => void mutate(state.starredAt === null ? "star" : "unstar"),
    [mutate, state.starredAt],
  );
  const handleArchive = useCallback(
    () => void mutate(state.location === "archive" ? "move_inbox" : "archive"),
    [mutate, state.location],
  );
  const handleAlreadyKnown = useCallback(() => void mutate("already_known"), [mutate]);
  const handleTonight = useCallback(
    () => void mutate("snooze", { snoozedUntil: snoozePresetTime("tonight", timezone) }),
    [mutate, timezone],
  );
  const handleTomorrow = useCallback(
    () => void mutate("snooze", { snoozedUntil: snoozePresetTime("tomorrow", timezone) }),
    [mutate, timezone],
  );
  const handleWeekend = useCallback(
    () => void mutate("snooze", { snoozedUntil: snoozePresetTime("weekend", timezone) }),
    [mutate, timezone],
  );
  const handleWeek = useCallback(
    () => void mutate("snooze", { snoozedUntil: snoozePresetTime("week", timezone) }),
    [mutate, timezone],
  );
  const handleUnsnooze = useCallback(() => void mutate("unsnooze"), [mutate]);

  const recordFeedback = useCallback(
    (type: StoryFeedbackType) => {
      if (feedbackPending) {
        return;
      }
      setFeedbackPending(true);
      setNotice("Recording relevance feedback…");
      void requestJSON(
        `/api/reading-state/stories/${encodeURIComponent(storyId)}/feedback`,
        "POST",
        { idempotencyKey: crypto.randomUUID(), type },
        parseFeedbackResponse,
      )
        .then(() => setNotice("Feedback recorded independently from reading state."))
        .catch((error: unknown) => {
          setNotice(error instanceof Error ? error.message : "Feedback could not be recorded.");
        })
        .finally(() => setFeedbackPending(false));
    },
    [feedbackPending, storyId],
  );

  const toggleTag = useCallback(
    (tagId: string) => {
      void mutate(state.tagIds.includes(tagId) ? "remove_tag" : "add_tag", { tagId });
    },
    [mutate, state.tagIds],
  );

  const deleteAnnotationByID = useCallback((annotationId: string) => {
    setNotice("Deleting annotation…");
    void fetch(`/api/reading-state/annotations/${encodeURIComponent(annotationId)}`, {
      method: "DELETE",
      signal: AbortSignal.timeout(7_000),
    })
      .then((response) => {
        if (!response.ok) {
          throw new Error("The annotation could not be deleted.");
        }
        setAnnotations((current) => current.filter((annotation) => annotation.id !== annotationId));
        setNotice("Annotation deleted.");
      })
      .catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "Annotation deletion failed.");
      });
  }, []);

  const noteForm = useForm({
    defaultValues: { body: "" },
    validators: { onSubmit: noteBodySchema },
    onSubmit: async ({ value }) => {
      const annotation = await requestJSON(
        `/api/reading-state/stories/${encodeURIComponent(storyId)}/annotations`,
        "POST",
        { body: value.body, revisionId, type: "document_note" },
        parseAnnotationResponse,
      );
      setAnnotations((current) => [...current, annotation]);
      noteForm.reset();
      setNotice("Document note saved.");
    },
  });

  const handleNoteSubmit = useCallback(
    (event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void noteForm.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The note could not be saved.");
      });
    },
    [noteForm],
  );

  const highlightForm = useForm({
    defaultValues: { body: "" },
    validators: { onSubmit: highlightBodySchema },
    onSubmit: async ({ value }) => {
      const span = selectedSourceSpan();
      if (span === null) {
        throw new Error("Select text in the normalized source before saving a highlight.");
      }
      const annotation = await requestJSON(
        `/api/reading-state/stories/${encodeURIComponent(storyId)}/annotations`,
        "POST",
        {
          body: value.body,
          endOffset: span.end,
          quoteHash: await digestHex(span.quote),
          revisionId,
          startOffset: span.start,
          type: value.body.trim() === "" ? "highlight" : "highlight_note",
        },
        parseAnnotationResponse,
      );
      setAnnotations((current) => [...current, annotation]);
      highlightForm.reset();
      window.getSelection()?.removeAllRanges();
      setNotice(
        value.body.trim() === "" ? "Revision-bound highlight saved." : "Highlight note saved.",
      );
    },
  });

  const handleHighlightSubmit = useCallback(
    (event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void highlightForm.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The highlight could not be saved.");
      });
    },
    [highlightForm],
  );

  const tagForm = useForm({
    defaultValues: { colorToken: "accent" as StoryTag["colorToken"], name: "" },
    validators: { onSubmit: tagInputSchema },
    onSubmit: async ({ value }) => {
      const tag = await requestJSON("/api/reading-state/tags", "POST", value, parseTagResponse);
      setTags((current) => [...current, tag]);
      tagForm.reset();
      setNotice("Tag created.");
    },
  });

  const dismissForm = useForm({
    defaultValues: { reason: "irrelevant_topic" as DismissalReason },
    validators: { onSubmit: dismissalFormSchema },
    onSubmit: async ({ value }) => {
      await mutate("dismiss", { dismissedReason: value.reason });
    },
  });

  const handleDismissSubmit = useCallback(
    (event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void dismissForm.handleSubmit();
    },
    [dismissForm],
  );

  const customSnoozeForm = useForm({
    defaultValues: { until: "" },
    validators: { onSubmit: customSnoozeFormSchema },
    onSubmit: async ({ value }) => {
      const snoozedUntil = customSnoozeTime(value.until, timezone);
      if (Date.parse(snoozedUntil) <= Date.now()) {
        throw new Error("Choose a future snooze time.");
      }
      await mutate("snooze", { snoozedUntil });
    },
  });

  const handleCustomSnoozeSubmit = useCallback(
    (event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void customSnoozeForm.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The snooze time is invalid.");
      });
    },
    [customSnoozeForm],
  );

  const handleTagSubmit = useCallback(
    (event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void tagForm.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The tag could not be created.");
      });
    },
    [tagForm],
  );

  const stateLabel = useMemo(() => {
    const read = state.isRead ? "Read" : "Unread";
    const star = state.starredAt === null ? "" : " · Starred";
    return `${read} · ${state.location}${star}`;
  }, [state.isRead, state.location, state.starredAt]);

  const handleCopyPrivateLink = useCallback(() => {
    void navigator.clipboard
      .writeText(window.location.href)
      .then(() => setNotice("Private story link copied."))
      .catch(() => setNotice("The private story link could not be copied."));
  }, []);

  const handleMarkdownExport = useCallback(() => {
    const markdown = buildStoryMarkdown({
      annotations,
      exportedAt: new Date().toISOString(),
      state,
      story,
      tags,
    });
    const href = URL.createObjectURL(new Blob([markdown], { type: "text/markdown;charset=utf-8" }));
    const download = document.createElement("a");
    download.download = `relantern-story-${story.id}.md`;
    download.href = href;
    download.click();
    URL.revokeObjectURL(href);
    setNotice("Markdown export prepared locally.");
  }, [annotations, state, story, tags]);

  return (
    <aside aria-labelledby="story-workspace-title" className="story-knowledge-workspace">
      <div className="reader-section-heading">
        <div>
          <p className="eyebrow">Private reading state</p>
          <h2 id="story-workspace-title">Triage and retain context</h2>
        </div>
        <span>{stateLabel}</span>
      </div>

      <div
        aria-label="Story state actions"
        className="triage-actions story-state-actions"
        role="toolbar"
      >
        <button disabled={pending} onClick={handleRead} type="button">
          {state.isRead ? "Mark unread" : "Mark read"}
        </button>
        <button disabled={pending} onClick={handleLater} type="button">
          {state.location === "later" ? "Move to Inbox" : "Read Later"}
        </button>
        <button disabled={pending} onClick={handleStar} type="button">
          {state.starredAt === null ? "Star" : "Unstar"}
        </button>
        <button disabled={pending} onClick={handleArchive} type="button">
          {state.location === "archive" ? "Restore" : "Archive"}
        </button>
        <button disabled={pending} onClick={handleAlreadyKnown} type="button">
          Already known
        </button>
        <button onClick={handleCopyPrivateLink} type="button">
          Copy private link
        </button>
        <button onClick={handleMarkdownExport} type="button">
          Export Markdown
        </button>
      </div>

      <form className="compact-command-form" onSubmit={handleDismissSubmit}>
        <dismissForm.Field name="reason">
          {(field) => (
            <label>
              Dismiss reason
              <select
                onBlur={field.handleBlur}
                onChange={(event) =>
                  field.handleChange(event.target.value as typeof field.state.value)
                }
                value={field.state.value}
              >
                <option value="irrelevant_topic">Irrelevant topic</option>
                <option value="duplicate">Duplicate</option>
                <option value="too_promotional">Too promotional</option>
                <option value="low_quality">Low quality</option>
                <option value="already_known">Already known</option>
              </select>
            </label>
          )}
        </dismissForm.Field>
        <button disabled={pending} type="submit">
          Dismiss and archive
        </button>
      </form>

      <details className="snooze-controls">
        <summary>{state.snoozedUntil === null ? "Snooze…" : "Change snooze…"}</summary>
        <div className="triage-actions">
          <button
            disabled={pending || state.location === "archive"}
            onClick={handleTonight}
            type="button"
          >
            Tonight
          </button>
          <button
            disabled={pending || state.location === "archive"}
            onClick={handleTomorrow}
            type="button"
          >
            Tomorrow morning
          </button>
          <button
            disabled={pending || state.location === "archive"}
            onClick={handleWeekend}
            type="button"
          >
            This weekend
          </button>
          <button
            disabled={pending || state.location === "archive"}
            onClick={handleWeek}
            type="button"
          >
            Next week
          </button>
          {state.snoozedUntil !== null && (
            <button disabled={pending} onClick={handleUnsnooze} type="button">
              Unsnooze now
            </button>
          )}
        </div>
        <form className="compact-command-form" onSubmit={handleCustomSnoozeSubmit}>
          <customSnoozeForm.Field name="until">
            {(field) => (
              <label>
                Custom local time
                <input
                  aria-invalid={field.state.meta.errors.length > 0}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="datetime-local"
                  value={field.state.value}
                />
              </label>
            )}
          </customSnoozeForm.Field>
          <button disabled={pending || state.location === "archive"} type="submit">
            Snooze
          </button>
        </form>
      </details>

      <fieldset className="story-feedback-panel">
        <legend>Relevance feedback</legend>
        <p>
          These signals tune usefulness without changing read, Later, star, or archive state.
          Already known above also marks this story read.
        </p>
        <div className="triage-actions">
          <FeedbackButton
            disabled={feedbackPending}
            label="Useful"
            onFeedback={recordFeedback}
            type="useful"
          />
          <FeedbackButton
            disabled={feedbackPending}
            label="Irrelevant"
            onFeedback={recordFeedback}
            type="irrelevant"
          />
          <FeedbackButton
            disabled={feedbackPending}
            label="Too shallow"
            onFeedback={recordFeedback}
            type="too_shallow"
          />
          <FeedbackButton
            disabled={feedbackPending}
            label="Too verbose"
            onFeedback={recordFeedback}
            type="too_verbose"
          />
          <FeedbackButton
            disabled={feedbackPending}
            label="Incorrect"
            onFeedback={recordFeedback}
            type="incorrect"
          />
        </div>
      </fieldset>

      <div className="story-tags-panel">
        <h3>Tags</h3>
        <div className="tag-chip-row">
          {tags.map((tag) => (
            <StoryTagToggle
              active={state.tagIds.includes(tag.id)}
              key={tag.id}
              onToggle={toggleTag}
              tag={tag}
            />
          ))}
        </div>
        <form className="inline-form" onSubmit={handleTagSubmit}>
          <tagForm.Field name="name">
            {(field) => (
              <label>
                New tag
                <input
                  aria-invalid={field.state.meta.errors.length > 0}
                  maxLength={80}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  placeholder="e.g. evaluate"
                  value={field.state.value}
                />
              </label>
            )}
          </tagForm.Field>
          <button type="submit">Create tag</button>
        </form>
      </div>

      <div className="annotation-panel">
        <div>
          <h3>Notes and highlights</h3>
          <p>Select reader text above to preserve a revision-bound highlight.</p>
        </div>
        <form className="note-form" onSubmit={handleNoteSubmit}>
          <noteForm.Field name="body">
            {(field) => (
              <label>
                Document note
                <textarea
                  aria-invalid={field.state.meta.errors.length > 0}
                  id="story-document-note"
                  maxLength={20_000}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  rows={4}
                  value={field.state.value}
                />
              </label>
            )}
          </noteForm.Field>
          <div className="triage-actions">
            <button type="submit">Save note</button>
          </div>
        </form>
        <form className="note-form" onSubmit={handleHighlightSubmit}>
          <highlightForm.Field name="body">
            {(field) => (
              <label>
                Highlight note (optional)
                <textarea
                  aria-invalid={field.state.meta.errors.length > 0}
                  maxLength={20_000}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  rows={2}
                  value={field.state.value}
                />
              </label>
            )}
          </highlightForm.Field>
          <div className="triage-actions">
            <button type="submit">Save selected passage</button>
          </div>
        </form>
        <ul className="annotation-list">
          {annotations.map((annotation) => (
            <AnnotationRow
              annotation={annotation}
              key={annotation.id}
              onDelete={deleteAnnotationByID}
            />
          ))}
        </ul>
      </div>

      <div aria-atomic="true" aria-live="polite" className="triage-notice" role="status">
        <span>{notice}</span>
        {undo !== null && (
          <button disabled={pending} onClick={handleUndo} type="button">
            Undo
          </button>
        )}
      </div>
    </aside>
  );
};

export const StoryWorkspace = memo<StoryWorkspaceProps>(StoryWorkspaceComponent);
StoryWorkspace.displayName = "StoryWorkspace";
