import type { ReadingAction, ReadingLocation, StoryReadingState } from "@relantern/domain";

type PredictionOptions = Readonly<{
  dismissedReason?: string;
  readingProgress?: number;
  snoozedUntil?: string;
  tagId?: string;
}>;

const withoutTag = (tagIDs: readonly string[], tagID: string): readonly string[] =>
  tagIDs.filter((candidate) => candidate !== tagID);

const withTag = (tagIDs: readonly string[], tagID: string): readonly string[] =>
  tagIDs.includes(tagID) ? tagIDs : [...tagIDs, tagID].sort();

export const predictReadingState = (
  current: StoryReadingState,
  action: ReadingAction,
  now: string,
  options: PredictionOptions = {},
): StoryReadingState => {
  let next: StoryReadingState = { ...current, tagIds: [...current.tagIds] };
  const move = (location: ReadingLocation): StoryReadingState => ({
    ...next,
    dismissedReason: null,
    laterPosition: location === "later" ? Date.parse(now) : null,
    location,
    snoozedFromLocation: null,
    snoozedUntil: null,
  });

  switch (action) {
    case "mark_read":
    case "already_known":
      next = { ...next, isRead: true, readAt: now };
      break;
    case "mark_unread":
      next = { ...next, isRead: false, readAt: null };
      break;
    case "move_inbox":
      next = move("inbox");
      break;
    case "move_later":
      next = move("later");
      break;
    case "archive":
      next = { ...move("archive"), dismissedReason: null };
      break;
    case "dismiss":
      next = { ...move("archive"), dismissedReason: options.dismissedReason ?? null };
      break;
    case "snooze": {
      if (current.location === "archive" || options.snoozedUntil === undefined) {
        return current;
      }
      next = {
        ...next,
        isRead: false,
        readAt: null,
        snoozedFromLocation: current.location,
        snoozedUntil: options.snoozedUntil,
      };
      break;
    }
    case "unsnooze":
      next = {
        ...next,
        isRead: false,
        readAt: null,
        snoozedFromLocation: null,
        snoozedUntil: null,
      };
      break;
    case "star":
      next = { ...next, starredAt: now };
      break;
    case "unstar":
      next = { ...next, starredAt: null };
      break;
    case "add_tag":
      next =
        options.tagId === undefined
          ? current
          : { ...next, tagIds: withTag(next.tagIds, options.tagId) };
      break;
    case "remove_tag":
      next =
        options.tagId === undefined
          ? current
          : { ...next, tagIds: withoutTag(next.tagIds, options.tagId) };
      break;
    case "update_progress":
      next =
        options.readingProgress === undefined
          ? current
          : { ...next, readingProgress: options.readingProgress };
      break;
  }
  return { ...next, updatedAt: now, version: current.version + 1 };
};

export const belongsToCollection = (
  kind: "archive" | "inbox" | "later" | "snoozed" | "starred" | "today",
  state: StoryReadingState,
  now: string,
): boolean => {
  const activelySnoozed =
    state.snoozedUntil !== null && Date.parse(state.snoozedUntil) > Date.parse(now);
  switch (kind) {
    case "archive":
      return state.location === "archive";
    case "inbox":
      return state.location === "inbox" && !activelySnoozed;
    case "later":
      return state.location === "later" && !activelySnoozed;
    case "snoozed":
      return activelySnoozed;
    case "starred":
      return state.starredAt !== null;
    case "today":
      return state.location !== "archive" && !activelySnoozed;
  }
};
