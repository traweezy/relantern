"use client";

import type {
  BulkReadingMutation,
  ReadingAction,
  ReadingCollection,
  ReadingMutation,
  StoryListItem,
  StoryTag,
} from "@relantern/domain";
import type { Route } from "next";
import Link from "next/link";
import type { ChangeEvent, ReactNode, SyntheticEvent } from "react";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { StoryCard } from "@/features/intelligence/story-card";
import type { ReadingMutationCommand } from "./commands";
import { parseBulkReadingMutation, parseReadingMutation } from "./contract";
import { belongsToCollection, predictReadingState } from "./optimistic";
import { snoozePresetTime } from "./snooze-time";

type CollectionKind = "archive" | "inbox" | "later" | "snoozed" | "starred" | "today";

type MutationOptions = Readonly<{
  dismissedReason?:
    | "already_known"
    | "duplicate"
    | "irrelevant_topic"
    | "low_quality"
    | "too_promotional";
  snoozedUntil?: string;
  tagId?: string;
}>;

type UndoEntry = Readonly<{
  index: number;
  item: StoryListItem;
  mutationId: string;
  storyId: string;
}>;

type UndoState = Readonly<{
  bulkId?: string;
  deadline: string;
  entries: readonly UndoEntry[];
}>;

type TriageCollectionProps = Readonly<{
  initialCollection: ReadingCollection;
  kind: CollectionKind;
  tags: readonly StoryTag[];
  timezone: string;
}>;

type RunAction = (storyID: string, action: ReadingAction, options?: MutationOptions) => void;
type RunBulkAction = (
  action: ReadingAction,
  storyIDs: readonly string[],
  options?: MutationOptions,
) => void;
type ReorderStory = (storyID: string, direction: -1 | 1) => void;
type ToggleSelection = (storyID: string, range?: boolean) => void;
type ToggleTag = (storyID: string, tagID: string, present: boolean) => void;

type CommandDialogProps = Readonly<{
  children: ReactNode;
  labelID: string;
  onClose: () => void;
}>;

const CommandDialogComponent = ({ children, labelID, onClose }: CommandDialogProps) => {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const returnFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (dialog === null) {
      return;
    }
    returnFocusRef.current = document.activeElement as HTMLElement | null;
    dialog.showModal();
    return () => {
      if (dialog.open) {
        dialog.close();
      }
      returnFocusRef.current?.focus();
    };
  }, []);

  const handleCancel = useCallback(
    (event: SyntheticEvent<HTMLDialogElement>) => {
      event.preventDefault();
      onClose();
    },
    [onClose],
  );

  return (
    <dialog
      aria-labelledby={labelID}
      className="command-panel"
      onCancel={handleCancel}
      ref={dialogRef}
    >
      {children}
    </dialog>
  );
};

const CommandDialog = memo<CommandDialogProps>(CommandDialogComponent);
CommandDialog.displayName = "CommandDialog";

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
        : "The command failed.";
    throw new Error(detail);
  }
  return parse(decoded);
};

const stateActionLabel = (item: StoryListItem): string => {
  if (item.state.snoozedUntil !== null) {
    return `Snoozed until ${new Intl.DateTimeFormat("en-US", {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(item.state.snoozedUntil))}`;
  }
  const read = item.state.isRead ? "Read" : "Unread";
  const starred = item.state.starredAt === null ? "" : " · Starred";
  return `${read} · ${item.state.location}${starred}`;
};

type TagToggleProps = Readonly<{
  active: boolean;
  onToggleTag: ToggleTag;
  storyID: string;
  tag: StoryTag;
}>;

const TagToggleComponent = ({ active, onToggleTag, storyID, tag }: TagToggleProps) => {
  const handleClick = useCallback(() => {
    onToggleTag(storyID, tag.id, active);
  }, [active, onToggleTag, storyID, tag.id]);
  return (
    <button
      aria-pressed={active}
      className={`tag-chip tag-${tag.colorToken}${active ? " tag-chip-active" : ""}`}
      data-story-tag
      onClick={handleClick}
      type="button"
    >
      {active ? "Remove " : "Add "}
      {tag.name}
    </button>
  );
};

const TagToggle = memo<TagToggleProps>(TagToggleComponent);
TagToggle.displayName = "TagToggle";

type BulkTagButtonProps = Readonly<{
  onBulkAction: RunBulkAction;
  selectedIDs: readonly string[];
  tag: StoryTag;
}>;

const BulkTagButtonComponent = ({ onBulkAction, selectedIDs, tag }: BulkTagButtonProps) => {
  const handleClick = useCallback(() => {
    onBulkAction("add_tag", selectedIDs, { tagId: tag.id });
  }, [onBulkAction, selectedIDs, tag.id]);
  return (
    <button disabled={selectedIDs.length === 0} onClick={handleClick} type="button">
      Tag {tag.name}
    </button>
  );
};

const BulkTagButton = memo<BulkTagButtonProps>(BulkTagButtonComponent);
BulkTagButton.displayName = "BulkTagButton";

type TriageCardProps = Readonly<{
  active: boolean;
  first: boolean;
  item: StoryListItem;
  last: boolean;
  onAction: RunAction;
  onReorder: ReorderStory | null;
  onToggleSelection: ToggleSelection;
  onToggleTag: ToggleTag;
  pending: boolean;
  selected: boolean;
  tags: readonly StoryTag[];
  timezone: string;
}>;

const TriageCardComponent = ({
  active,
  first,
  item,
  last,
  onAction,
  onReorder,
  onToggleSelection,
  onToggleTag,
  pending,
  selected,
  tags,
  timezone,
}: TriageCardProps) => {
  const storyID = item.story.id;
  const handleSelection = useCallback(
    (event: ChangeEvent<HTMLInputElement>) => {
      const nativeEvent = event.nativeEvent;
      onToggleSelection(storyID, nativeEvent instanceof MouseEvent && nativeEvent.shiftKey);
    },
    [onToggleSelection, storyID],
  );
  const handleRead = useCallback(() => {
    onAction(storyID, item.state.isRead ? "mark_unread" : "mark_read");
  }, [item.state.isRead, onAction, storyID]);
  const handleLater = useCallback(() => {
    onAction(storyID, item.state.location === "later" ? "move_inbox" : "move_later");
  }, [item.state.location, onAction, storyID]);
  const handleStar = useCallback(() => {
    onAction(storyID, item.state.starredAt === null ? "star" : "unstar");
  }, [item.state.starredAt, onAction, storyID]);
  const handleArchive = useCallback(() => {
    onAction(storyID, item.state.location === "archive" ? "move_inbox" : "archive");
  }, [item.state.location, onAction, storyID]);
  const handleSnoozeTonight = useCallback(() => {
    onAction(storyID, "snooze", { snoozedUntil: snoozePresetTime("tonight", timezone) });
  }, [onAction, storyID, timezone]);
  const handleSnoozeTomorrow = useCallback(() => {
    onAction(storyID, "snooze", { snoozedUntil: snoozePresetTime("tomorrow", timezone) });
  }, [onAction, storyID, timezone]);
  const handleSnoozeWeekend = useCallback(() => {
    onAction(storyID, "snooze", { snoozedUntil: snoozePresetTime("weekend", timezone) });
  }, [onAction, storyID, timezone]);
  const handleSnoozeWeek = useCallback(() => {
    onAction(storyID, "snooze", { snoozedUntil: snoozePresetTime("week", timezone) });
  }, [onAction, storyID, timezone]);
  const handleUnsnooze = useCallback(() => {
    onAction(storyID, "unsnooze");
  }, [onAction, storyID]);
  const handleAlreadyKnown = useCallback(() => {
    onAction(storyID, "already_known");
  }, [onAction, storyID]);
  const handleDismiss = useCallback(() => {
    onAction(storyID, "dismiss");
  }, [onAction, storyID]);
  const handleSource = useCallback(() => {
    window.open(item.story.primarySourceUrl, "_blank", "noopener,noreferrer");
  }, [item.story.primarySourceUrl]);
  const handleMoveUp = useCallback(() => {
    onReorder?.(storyID, -1);
  }, [onReorder, storyID]);
  const handleMoveDown = useCallback(() => {
    onReorder?.(storyID, 1);
  }, [onReorder, storyID]);

  return (
    <div
      aria-current={active ? "true" : undefined}
      className={`triage-card${active ? " triage-card-active" : ""}${pending ? " triage-card-pending" : ""}`}
      data-triage-card={storyID}
      tabIndex={-1}
    >
      <div className="triage-selection-row">
        <label>
          <input checked={selected} onChange={handleSelection} type="checkbox" />
          <span>Select story</span>
        </label>
        <span>{stateActionLabel(item)}</span>
      </div>
      <StoryCard href={`/story/${storyID}`} story={item.story} timezone={timezone} />
      <div aria-label="Story state actions" className="triage-actions" role="toolbar">
        <button disabled={pending} onClick={handleRead} type="button">
          {item.state.isRead ? "Unread" : "Read"}
        </button>
        <button disabled={pending} onClick={handleLater} type="button">
          {item.state.location === "later" ? "Inbox" : "Later"}
        </button>
        <button disabled={pending} onClick={handleStar} type="button">
          {item.state.starredAt === null ? "Star" : "Unstar"}
        </button>
        {item.state.snoozedUntil === null ? (
          <details className="card-snooze-controls" data-snooze-menu>
            <summary>Snooze…</summary>
            <fieldset className="card-snooze-menu">
              <legend className="sr-only">Snooze presets</legend>
              <button disabled={pending} onClick={handleSnoozeTonight} type="button">
                Tonight
              </button>
              <button disabled={pending} onClick={handleSnoozeTomorrow} type="button">
                Tomorrow
              </button>
              <button disabled={pending} onClick={handleSnoozeWeekend} type="button">
                Weekend
              </button>
              <button disabled={pending} onClick={handleSnoozeWeek} type="button">
                Next week
              </button>
            </fieldset>
          </details>
        ) : (
          <button data-snooze-toggle disabled={pending} onClick={handleUnsnooze} type="button">
            Unsnooze
          </button>
        )}
        <button disabled={pending} onClick={handleArchive} type="button">
          {item.state.location === "archive" ? "Restore" : "Archive"}
        </button>
        <button disabled={pending} onClick={handleAlreadyKnown} type="button">
          Known
        </button>
        <button disabled={pending} onClick={handleDismiss} type="button">
          Dismiss
        </button>
        <button disabled={pending} onClick={handleSource} type="button">
          Source
        </button>
        {onReorder !== null && (
          <>
            <button disabled={pending || first} onClick={handleMoveUp} type="button">
              Move up
            </button>
            <button disabled={pending || last} onClick={handleMoveDown} type="button">
              Move down
            </button>
          </>
        )}
      </div>
      {tags.length > 0 && (
        <fieldset className="tag-chip-row">
          <legend className="sr-only">Story tags</legend>
          {tags.map((tag) => (
            <TagToggle
              active={item.state.tagIds.includes(tag.id)}
              key={tag.id}
              onToggleTag={onToggleTag}
              storyID={storyID}
              tag={tag}
            />
          ))}
        </fieldset>
      )}
    </div>
  );
};

const TriageCard = memo<TriageCardProps>(TriageCardComponent);
TriageCard.displayName = "TriageCard";

const TriageCollectionComponent = ({
  initialCollection,
  kind,
  tags,
  timezone,
}: TriageCollectionProps) => {
  const [items, setItems] = useState<readonly StoryListItem[]>(initialCollection.items);
  const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set());
  const [pending, setPending] = useState<ReadonlySet<string>>(() => new Set());
  const [activeIndex, setActiveIndex] = useState(0);
  const [notice, setNotice] = useState("");
  const [exportPending, setExportPending] = useState(false);
  const [undo, setUndo] = useState<UndoState | null>(null);
  const [commandOpen, setCommandOpen] = useState(false);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const goPrefixAt = useRef(0);
  const lastSelectedIndex = useRef<number | null>(null);

  const itemByID = useMemo(
    () => new Map(items.map((item, index) => [item.story.id, { index, item }] as const)),
    [items],
  );

  const focusIndex = useCallback(
    (index: number) => {
      if (items.length === 0) {
        return;
      }
      const bounded = Math.max(0, Math.min(index, items.length - 1));
      setActiveIndex(bounded);
      const storyID = items[bounded]?.story.id;
      if (storyID !== undefined) {
        requestAnimationFrame(() => {
          document.querySelector<HTMLElement>(`[data-triage-card="${storyID}"]`)?.focus();
        });
      }
    },
    [items],
  );

  const toggleSelection = useCallback<ToggleSelection>(
    (storyID, range = false) => {
      const target = itemByID.get(storyID);
      if (target === undefined) {
        return;
      }
      setSelected((current) => {
        const next = new Set(current);
        if (range && lastSelectedIndex.current !== null) {
          const start = Math.min(lastSelectedIndex.current, target.index);
          const end = Math.max(lastSelectedIndex.current, target.index);
          for (let index = start; index <= end; index += 1) {
            const item = items[index];
            if (item !== undefined) {
              next.add(item.story.id);
            }
          }
        } else if (next.has(storyID)) {
          next.delete(storyID);
        } else {
          next.add(storyID);
        }
        return next;
      });
      lastSelectedIndex.current = target.index;
    },
    [itemByID, items],
  );

  const executeAction = useCallback<RunAction>(
    (storyID, action, options = {}) => {
      const target = itemByID.get(storyID);
      if (target === undefined || pending.has(storyID)) {
        return;
      }
      const original = target.item;
      const now = new Date().toISOString();
      const predicted = predictReadingState(original.state, action, now, options);
      const command: ReadingMutationCommand = {
        action,
        idempotencyKey: crypto.randomUUID(),
        version: original.state.version,
        ...(options.dismissedReason === undefined
          ? {}
          : { dismissedReason: options.dismissedReason }),
        ...(options.snoozedUntil === undefined ? {} : { snoozedUntil: options.snoozedUntil }),
        ...(options.tagId === undefined ? {} : { tagId: options.tagId }),
      };
      setPending((current) => new Set(current).add(storyID));
      setItems((current) =>
        current.map((item) => (item.story.id === storyID ? { ...item, state: predicted } : item)),
      );
      setNotice("Saving command…");
      void requestJSON(
        `/api/reading-state/stories/${encodeURIComponent(storyID)}/state`,
        "PATCH",
        command,
        parseReadingMutation,
      )
        .then((mutation) => {
          const reconciledAt = new Date().toISOString();
          setItems((current) =>
            current
              .map((item) =>
                item.story.id === storyID ? { ...item, state: mutation.state } : item,
              )
              .filter((item) => belongsToCollection(kind, item.state, reconciledAt)),
          );
          setSelected((current) => {
            const next = new Set(current);
            next.delete(storyID);
            return next;
          });
          setUndo({
            deadline: mutation.undoDeadline,
            entries: [
              {
                index: target.index,
                item: original,
                mutationId: mutation.mutationId,
                storyId: storyID,
              },
            ],
          });
          setNotice("Command saved. Undo is available for ten seconds.");
        })
        .catch((error: unknown) => {
          setItems((current) =>
            current.map((item) => (item.story.id === storyID ? original : item)),
          );
          setNotice(
            error instanceof Error ? error.message : "The command failed and was rolled back.",
          );
        })
        .finally(() => {
          setPending((current) => {
            const next = new Set(current);
            next.delete(storyID);
            return next;
          });
        });
    },
    [itemByID, kind, pending],
  );

  const toggleTag = useCallback<ToggleTag>(
    (storyID, tagID, present) => {
      executeAction(storyID, present ? "remove_tag" : "add_tag", { tagId: tagID });
    },
    [executeAction],
  );

  const executeBulk = useCallback<RunBulkAction>(
    (action: ReadingAction, storyIDs: readonly string[], options: MutationOptions = {}) => {
      const targets = storyIDs
        .map((storyID) => itemByID.get(storyID))
        .filter((target): target is NonNullable<typeof target> => target !== undefined);
      if (targets.length === 0) {
        return;
      }
      if (
        (action === "archive" || action === "dismiss" || targets.length > 20) &&
        !window.confirm(
          `Apply ${action.replaceAll("_", " ")} to exactly ${targets.length} stories?`,
        )
      ) {
        return;
      }
      const now = new Date().toISOString();
      const snoozedUntil =
        action === "snooze"
          ? (options.snoozedUntil ?? snoozePresetTime("tomorrow", timezone))
          : undefined;
      const targetIDs = new Set(targets.map(({ item }) => item.story.id));
      setPending((current) => new Set([...current, ...targetIDs]));
      setItems((current) =>
        current.map((item) =>
          targetIDs.has(item.story.id)
            ? {
                ...item,
                state: predictReadingState(item.state, action, now, {
                  ...(options.dismissedReason === undefined
                    ? {}
                    : { dismissedReason: options.dismissedReason }),
                  ...(snoozedUntil === undefined ? {} : { snoozedUntil }),
                  ...(options.tagId === undefined ? {} : { tagId: options.tagId }),
                }),
              }
            : item,
        ),
      );
      setNotice(`Saving ${targets.length} commands…`);
      void requestJSON(
        "/api/reading-state/bulk",
        "POST",
        {
          action,
          expectedCount: targets.length,
          idempotencyKey: crypto.randomUUID(),
          items: targets.map(({ item }) => ({
            storyId: item.story.id,
            version: item.state.version,
          })),
          ...(options.dismissedReason === undefined
            ? {}
            : { dismissedReason: options.dismissedReason }),
          ...(snoozedUntil === undefined ? {} : { snoozedUntil }),
          ...(options.tagId === undefined ? {} : { tagId: options.tagId }),
        },
        parseBulkReadingMutation,
      )
        .then((mutation) => {
          const stateByID = new Map(
            mutation.mutations.map((entry) => [entry.state.storyId, entry.state] as const),
          );
          const reconciledAt = new Date().toISOString();
          setItems((current) =>
            current
              .map((item) => {
                const state = stateByID.get(item.story.id);
                return state === undefined ? item : { ...item, state };
              })
              .filter((item) => belongsToCollection(kind, item.state, reconciledAt)),
          );
          setSelected(new Set());
          setUndo({
            bulkId: mutation.bulkId,
            deadline: mutation.undoDeadline,
            entries: mutation.mutations.flatMap((entry) => {
              const original = itemByID.get(entry.state.storyId);
              return original === undefined
                ? []
                : [
                    {
                      index: original.index,
                      item: original.item,
                      mutationId: entry.mutationId,
                      storyId: entry.state.storyId,
                    },
                  ];
            }),
          });
          setNotice(
            `${mutation.affectedCount} stories updated. Undo is available for ten seconds.`,
          );
        })
        .catch((error: unknown) => {
          const originalByID = new Map(targets.map(({ item }) => [item.story.id, item] as const));
          setItems((current) => current.map((item) => originalByID.get(item.story.id) ?? item));
          setNotice(error instanceof Error ? error.message : "The bulk command was rolled back.");
        })
        .finally(() => {
          setPending((current) => {
            const next = new Set(current);
            for (const storyID of targetIDs) {
              next.delete(storyID);
            }
            return next;
          });
        });
    },
    [itemByID, kind, timezone],
  );

  const reorderStory = useCallback<ReorderStory>(
    (storyID, direction) => {
      if (kind !== "later" || pending.size > 0) {
        return;
      }
      const from = items.findIndex((item) => item.story.id === storyID);
      const to = from + direction;
      if (from < 0 || to < 0 || to >= items.length) {
        return;
      }
      const original = [...items];
      const ordered = [...items];
      const source = ordered[from];
      const destination = ordered[to];
      if (source === undefined || destination === undefined) {
        return;
      }
      ordered[from] = destination;
      ordered[to] = source;
      const targetIDs = new Set(ordered.map((item) => item.story.id));
      setItems(ordered);
      setActiveIndex(to);
      setPending(targetIDs);
      setNotice(`Saving the visible Later order for exactly ${ordered.length} stories…`);
      void requestJSON(
        "/api/reading-state/later/order",
        "PUT",
        {
          expectedCount: ordered.length,
          idempotencyKey: crypto.randomUUID(),
          items: ordered.map((item) => ({ storyId: item.story.id, version: item.state.version })),
        },
        parseBulkReadingMutation,
      )
        .then((mutation) => {
          const stateByID = new Map(
            mutation.mutations.map((entry) => [entry.state.storyId, entry.state] as const),
          );
          setItems((current) =>
            current.map((item) => ({
              ...item,
              state: stateByID.get(item.story.id) ?? item.state,
            })),
          );
          setUndo({
            bulkId: mutation.bulkId,
            deadline: mutation.undoDeadline,
            entries: mutation.mutations.flatMap((entry) => {
              const index = original.findIndex((item) => item.story.id === entry.state.storyId);
              const item = original[index];
              return item === undefined
                ? []
                : [{ index, item, mutationId: entry.mutationId, storyId: entry.state.storyId }];
            }),
          });
          setNotice("Later order saved. Undo is available for ten seconds.");
        })
        .catch((error: unknown) => {
          setItems(original);
          setActiveIndex(from);
          setNotice(error instanceof Error ? error.message : "The Later order was rolled back.");
        })
        .finally(() => setPending(new Set()));
    },
    [items, kind, pending.size],
  );

  const executeSelectedRead = useCallback(() => {
    executeBulk("mark_read", [...selected]);
  }, [executeBulk, selected]);
  const executeSelectedUnread = useCallback(() => {
    executeBulk("mark_unread", [...selected]);
  }, [executeBulk, selected]);
  const executeSelectedLater = useCallback(() => {
    executeBulk("move_later", [...selected]);
  }, [executeBulk, selected]);
  const executeSelectedStar = useCallback(() => {
    executeBulk("star", [...selected]);
  }, [executeBulk, selected]);
  const executeSelectedUnstar = useCallback(() => {
    executeBulk("unstar", [...selected]);
  }, [executeBulk, selected]);
  const executeSelectedSnooze = useCallback(() => {
    executeBulk("snooze", [...selected]);
  }, [executeBulk, selected]);
  const executeSelectedArchive = useCallback(() => {
    executeBulk("archive", [...selected]);
  }, [executeBulk, selected]);
  const executeSelectedDismiss = useCallback(() => {
    executeBulk("dismiss", [...selected]);
  }, [executeBulk, selected]);
  const selectVisible = useCallback(() => {
    setSelected(new Set(items.map((item) => item.story.id)));
  }, [items]);
  const clearSelection = useCallback(() => setSelected(new Set()), []);
  const selectedIncludesArchive = useMemo(
    () => [...selected].some((storyID) => itemByID.get(storyID)?.item.state.location === "archive"),
    [itemByID, selected],
  );
  const selectedIDs = useMemo(() => [...selected], [selected]);
  const exportSelectedMarkdown = useCallback(() => {
    if (selectedIDs.length < 1 || selectedIDs.length > 100 || exportPending) {
      setNotice("Select between 1 and 100 stories for a Markdown export.");
      return;
    }
    setExportPending(true);
    setNotice(`Preparing a Markdown export for ${selectedIDs.length} stories…`);
    void fetch("/api/discovery/exports/markdown", {
      body: JSON.stringify({ storyIds: selectedIDs }),
      headers: { accept: "text/markdown", "content-type": "application/json" },
      method: "POST",
      signal: AbortSignal.timeout(15_000),
    })
      .then(async (response) => {
        if (!response.ok) {
          const problem: unknown = await response.json().catch(() => null);
          const detail =
            typeof problem === "object" && problem !== null && "detail" in problem
              ? String(problem.detail)
              : "The Markdown export could not be prepared.";
          throw new Error(detail);
        }
        if (!response.headers.get("content-type")?.startsWith("text/markdown")) {
          throw new Error("The export response did not contain Markdown.");
        }
        const objectURL = URL.createObjectURL(await response.blob());
        const link = document.createElement("a");
        try {
          link.download = "relantern-stories.md";
          link.href = objectURL;
          document.body.append(link);
          link.click();
          setNotice(`Markdown export prepared for ${selectedIDs.length} stories.`);
        } finally {
          link.remove();
          URL.revokeObjectURL(objectURL);
        }
      })
      .catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The Markdown export failed.");
      })
      .finally(() => setExportPending(false));
  }, [exportPending, selectedIDs]);

  const handleUndo = useCallback(() => {
    if (undo === null) {
      return;
    }
    const undoRequest: Promise<ReadingMutation | BulkReadingMutation> =
      undo.bulkId === undefined
        ? requestJSON(
            `/api/reading-state/stories/${encodeURIComponent(undo.entries[0]?.storyId ?? "")}/undo`,
            "POST",
            { mutationId: undo.entries[0]?.mutationId },
            parseReadingMutation,
          )
        : requestJSON(
            "/api/reading-state/bulk/undo",
            "POST",
            { bulkId: undo.bulkId },
            parseBulkReadingMutation,
          );
    setNotice("Restoring the prior state…");
    void undoRequest
      .then((result) => {
        const mutations = "mutations" in result ? result.mutations : [result];
        const states = new Map(
          mutations.map((entry) => [entry.state.storyId, entry.state] as const),
        );
        setItems((current) => {
          const merged = [...current];
          for (const entry of undo.entries) {
            const state = states.get(entry.storyId);
            if (state === undefined) {
              continue;
            }
            const restored = { ...entry.item, state };
            const existing = merged.findIndex((item) => item.story.id === entry.storyId);
            if (existing >= 0) {
              merged[existing] = restored;
            } else if (belongsToCollection(kind, state, new Date().toISOString())) {
              merged.splice(Math.min(entry.index, merged.length), 0, restored);
            }
          }
          return merged;
        });
        setUndo(null);
        setNotice("Prior state restored.");
      })
      .catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "Undo could not be completed.");
      });
  }, [kind, undo]);

  useEffect(() => {
    if (undo === null) {
      return;
    }
    const remaining = Math.max(0, Date.parse(undo.deadline) - Date.now());
    const timer = window.setTimeout(() => setUndo(null), remaining);
    return () => window.clearTimeout(timer);
  }, [undo]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const target = event.target;
      const typing =
        target instanceof HTMLInputElement ||
        target instanceof HTMLTextAreaElement ||
        target instanceof HTMLSelectElement ||
        (target instanceof HTMLElement && target.isContentEditable);
      if (typing || window.getSelection()?.isCollapsed === false) {
        return;
      }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setCommandOpen((current) => !current);
        return;
      }
      if (event.key === "?") {
        event.preventDefault();
        setShortcutsOpen((current) => !current);
        return;
      }
      const key = event.key.toLowerCase();
      if (key === "g") {
        goPrefixAt.current = Date.now();
        return;
      }
      if (Date.now() - goPrefixAt.current < 1_000) {
        const destinations: Readonly<Record<string, string>> = {
          i: "/inbox",
          l: "/later",
          r: "/radar",
          s: "/starred",
          t: "/",
        };
        const destination = destinations[key];
        if (destination !== undefined) {
          event.preventDefault();
          window.location.assign(destination);
          return;
        }
      }
      if (event.shiftKey && key === "a") {
        event.preventDefault();
        const visible = items.map((item) => item.story.id);
        if (window.confirm(`Mark exactly ${visible.length} visible stories read?`)) {
          executeBulk("mark_read", visible);
        }
        return;
      }
      const active = items[activeIndex];
      if (active === undefined) {
        return;
      }
      const storyID = active.story.id;
      switch (key) {
        case "j":
          event.preventDefault();
          focusIndex(activeIndex + 1);
          break;
        case "k":
          event.preventDefault();
          focusIndex(activeIndex - 1);
          break;
        case "enter":
        case "o":
          event.preventDefault();
          window.location.assign(`/story/${storyID}`);
          break;
        case "v":
          event.preventDefault();
          window.open(active.story.primarySourceUrl, "_blank", "noopener,noreferrer");
          break;
        case "m":
          event.preventDefault();
          executeAction(storyID, active.state.isRead ? "mark_unread" : "mark_read");
          break;
        case "l":
          event.preventDefault();
          executeAction(storyID, active.state.location === "later" ? "move_inbox" : "move_later");
          break;
        case "s":
          event.preventDefault();
          executeAction(storyID, active.state.starredAt === null ? "star" : "unstar");
          break;
        case "e":
          event.preventDefault();
          executeAction(storyID, "archive");
          break;
        case "z":
          event.preventDefault();
          {
            const card = document.querySelector<HTMLElement>(`[data-triage-card="${storyID}"]`);
            const menu = card?.querySelector<HTMLDetailsElement>("[data-snooze-menu]");
            if (menu !== null && menu !== undefined) {
              menu.open = true;
              menu.querySelector<HTMLElement>("summary")?.focus();
            } else {
              card?.querySelector<HTMLElement>("[data-snooze-toggle]")?.focus();
            }
          }
          break;
        case "t":
          event.preventDefault();
          document
            .querySelector<HTMLElement>(`[data-triage-card="${storyID}"] [data-story-tag]`)
            ?.focus();
          break;
        case "x":
          event.preventDefault();
          toggleSelection(storyID);
          break;
        case "n":
          event.preventDefault();
          window.location.assign(`/story/${storyID}?note=1`);
          break;
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [activeIndex, executeAction, executeBulk, focusIndex, items, toggleSelection]);

  const closeCommand = useCallback(() => setCommandOpen(false), []);
  const closeShortcuts = useCallback(() => setShortcutsOpen(false), []);

  if (items.length === 0) {
    return (
      <section className="reading-empty-state">
        <p className="eyebrow">Collection clear</p>
        <h2>No stories match this view.</h2>
        <p>New evidence and restored snoozes will appear here automatically.</p>
        <p aria-live="polite" role="status">
          {notice}
        </p>
        {undo !== null && (
          <button className="primary-button" onClick={handleUndo} type="button">
            Undo last command
          </button>
        )}
      </section>
    );
  }

  return (
    <>
      <div className="triage-toolbar">
        <div>
          <strong>{selected.size}</strong> selected · {initialCollection.total} total
        </div>
        <div aria-label="Bulk story actions" className="triage-actions" role="toolbar">
          <button disabled={selected.size === 0} onClick={executeSelectedRead} type="button">
            Read
          </button>
          <button disabled={selected.size === 0} onClick={executeSelectedUnread} type="button">
            Unread
          </button>
          <button disabled={selected.size === 0} onClick={executeSelectedLater} type="button">
            Later
          </button>
          <button disabled={selected.size === 0} onClick={executeSelectedStar} type="button">
            Star
          </button>
          <button disabled={selected.size === 0} onClick={executeSelectedUnstar} type="button">
            Unstar
          </button>
          <button
            disabled={selected.size === 0 || selectedIncludesArchive}
            onClick={executeSelectedSnooze}
            type="button"
          >
            Snooze
          </button>
          <button disabled={selected.size === 0} onClick={executeSelectedArchive} type="button">
            Archive
          </button>
          <button disabled={selected.size === 0} onClick={executeSelectedDismiss} type="button">
            Dismiss
          </button>
          <button
            disabled={selected.size === 0 || selected.size > 100 || exportPending}
            onClick={exportSelectedMarkdown}
            title="Export 1 through 100 selected stories with notes, highlights, and citations"
            type="button"
          >
            {exportPending ? "Exporting…" : "Export Markdown"}
          </button>
          <button disabled={selected.size === items.length} onClick={selectVisible} type="button">
            Select {items.length} visible
          </button>
          <button disabled={selected.size === 0} onClick={clearSelection} type="button">
            Clear
          </button>
        </div>
        {tags.length > 0 && (
          <div aria-label="Bulk tag actions" className="triage-actions" role="toolbar">
            {tags.map((tag) => (
              <BulkTagButton
                key={tag.id}
                onBulkAction={executeBulk}
                selectedIDs={selectedIDs}
                tag={tag}
              />
            ))}
          </div>
        )}
      </div>
      <div aria-atomic="true" aria-live="polite" className="triage-notice" role="status">
        <span>{notice}</span>
        {undo !== null && (
          <button onClick={handleUndo} type="button">
            Undo
          </button>
        )}
      </div>
      {commandOpen && (
        <CommandDialog labelID="command-palette-title" onClose={closeCommand}>
          <div>
            <p className="eyebrow">Command palette</p>
            <h2 id="command-palette-title">Go to</h2>
          </div>
          <nav aria-label="Command destinations">
            <Link href="/">Today</Link>
            <Link href={"/inbox" as Route}>Inbox</Link>
            <Link href={"/later" as Route}>Later</Link>
            <Link href={"/starred" as Route}>Starred</Link>
          </nav>
          <button onClick={closeCommand} type="button">
            Close
          </button>
        </CommandDialog>
      )}
      {shortcutsOpen && (
        <CommandDialog labelID="shortcut-reference-title" onClose={closeShortcuts}>
          <div>
            <p className="eyebrow">Keyboard</p>
            <h2 id="shortcut-reference-title">Collection shortcuts</h2>
          </div>
          <p>
            J/K move · Enter opens · M read · L Later · S star · E archive · Z snooze · X select
          </p>
          <p>Shift+A reads visible · G then I/L/S/T navigates · Ctrl/Command+K opens commands</p>
          <button onClick={closeShortcuts} type="button">
            Close
          </button>
        </CommandDialog>
      )}
      <div className="triage-list">
        {items.map((item, index) => (
          <TriageCard
            active={index === activeIndex}
            first={index === 0}
            item={item}
            key={item.story.id}
            last={index === items.length - 1}
            onAction={executeAction}
            onReorder={kind === "later" ? reorderStory : null}
            onToggleSelection={toggleSelection}
            onToggleTag={toggleTag}
            pending={pending.has(item.story.id)}
            selected={selected.has(item.story.id)}
            tags={tags}
            timezone={timezone}
          />
        ))}
      </div>
      {initialCollection.nextCursor !== null && (
        <Link
          className="secondary-button collection-next-link"
          href={`/${kind}?cursor=${encodeURIComponent(initialCollection.nextCursor)}` as Route}
        >
          Next page
        </Link>
      )}
    </>
  );
};

export const TriageCollection = memo<TriageCollectionProps>(TriageCollectionComponent);
TriageCollection.displayName = "TriageCollection";
