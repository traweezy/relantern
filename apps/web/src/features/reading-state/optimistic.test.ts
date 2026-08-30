import type { StoryReadingState } from "@relantern/domain";
import { describe, expect, it } from "vitest";
import { belongsToCollection, predictReadingState } from "./optimistic";

const now = "2026-08-29T12:00:00.000Z";

const fixtureState = (): StoryReadingState => ({
  dismissedReason: null,
  isRead: false,
  lastParagraphId: "paragraph-2",
  laterPosition: null,
  location: "inbox",
  readAt: null,
  readingProgress: 0.25,
  snoozedFromLocation: null,
  snoozedUntil: null,
  starredAt: null,
  storyId: "01991234-5678-7abc-8def-0123456789ab",
  tagIds: ["01991234-5678-7abc-8def-0123456789ac"],
  updatedAt: now,
  version: 3,
});

describe("optimistic reading state", () => {
  it("keeps star and knowledge state independent while archiving", () => {
    const starred = predictReadingState(fixtureState(), "star", now);
    const archived = predictReadingState(starred, "archive", now);
    expect(archived).toMatchObject({
      lastParagraphId: "paragraph-2",
      location: "archive",
      readingProgress: 0.25,
      starredAt: now,
      tagIds: ["01991234-5678-7abc-8def-0123456789ac"],
    });
  });

  it("hides active snoozes and restores their collection membership", () => {
    const tomorrow = "2026-08-30T12:00:00.000Z";
    const snoozed = predictReadingState(fixtureState(), "snooze", now, {
      snoozedUntil: tomorrow,
    });
    expect(belongsToCollection("inbox", snoozed, now)).toBe(false);
    expect(belongsToCollection("snoozed", snoozed, now)).toBe(true);
    const restored = predictReadingState(snoozed, "unsnooze", now);
    expect(belongsToCollection("inbox", restored, now)).toBe(true);
    expect(restored.isRead).toBe(false);
  });

  it("adds and removes tags without changing location", () => {
    const tagID = "01991234-5678-7abc-8def-0123456789ad";
    const tagged = predictReadingState(fixtureState(), "add_tag", now, { tagId: tagID });
    expect(tagged.location).toBe("inbox");
    expect(tagged.tagIds).toContain(tagID);
    expect(predictReadingState(tagged, "remove_tag", now, { tagId: tagID }).tagIds).not.toContain(
      tagID,
    );
  });
});
