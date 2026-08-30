import type { StoryDetail, StoryReadingState } from "@relantern/domain";
import { describe, expect, test } from "vitest";
import { demoSnapshot } from "../demo/demo-snapshot";
import { buildStoryMarkdown } from "./story-export";

const story = demoSnapshot.stories["react-actions-transition"] as StoryDetail;

const state: StoryReadingState = {
  dismissedReason: null,
  isRead: true,
  lastParagraphId: null,
  laterPosition: null,
  location: "later",
  readAt: "2026-08-29T12:00:00Z",
  readingProgress: 0.5,
  snoozedFromLocation: null,
  snoozedUntil: null,
  starredAt: null,
  storyId: story.id,
  tagIds: ["01990000-0000-7000-8000-000000000009"],
  updatedAt: "2026-08-29T12:00:00Z",
  version: 3,
};

describe("story Markdown export", () => {
  test("includes evidence, private state, and revision-bound annotation context", () => {
    const markdown = buildStoryMarkdown({
      annotations: [
        {
          body: "Compare the migration path.",
          createdAt: "2026-08-29T12:00:00Z",
          endOffset: 5,
          id: "01990000-0000-7000-8000-000000000010",
          orphaned: false,
          quoteHash: "a".repeat(64),
          revisionId: story.revisionId,
          startOffset: 0,
          storyId: story.id,
          type: "highlight_note",
          updatedAt: "2026-08-29T12:00:00Z",
        },
      ],
      exportedAt: "2026-08-29T12:00:00.000Z",
      state,
      story,
      tags: [
        {
          colorToken: "accent",
          createdAt: "2026-08-29T12:00:00Z",
          id: "01990000-0000-7000-8000-000000000009",
          name: "Evaluate",
          updatedAt: "2026-08-29T12:00:00Z",
        },
      ],
    });
    expect(markdown).toContain(`# ${story.headline}`);
    expect(markdown).toContain("Reading state: read, later");
    expect(markdown).toContain("Tags: Evaluate");
    expect(markdown).toContain("Compare the migration path.");
    expect(markdown).toContain("React documentation");
  });
});
