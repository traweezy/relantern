import { describe, expect, it } from "vitest";
import {
  parseAnnotationResponse,
  parseFeedbackResponse,
  parseStoryReadingState,
  ReadingStateContractError,
} from "./contract";

const storyID = "01991234-5678-7abc-8def-0123456789ab";
const tagID = "01991234-5678-7abc-8def-0123456789ac";
const timestamp = "2026-08-29T12:00:00Z";

const validState = (): Readonly<Record<string, unknown>> => ({
  dismissedReason: null,
  isRead: false,
  lastParagraphId: null,
  laterPosition: null,
  location: "inbox",
  readAt: null,
  readingProgress: 0,
  snoozedFromLocation: null,
  snoozedUntil: null,
  starredAt: null,
  storyId: storyID,
  tagIds: [tagID],
  updatedAt: timestamp,
  version: 0,
});

describe("reading-state response contracts", () => {
  it("accepts a consistent owner state", () => {
    expect(parseStoryReadingState(validState())).toMatchObject({
      isRead: false,
      storyId: storyID,
      tagIds: [tagID],
    });
  });

  it("rejects inconsistent read, snooze, and tag invariants", () => {
    expect(() => parseStoryReadingState({ ...validState(), isRead: true })).toThrow(
      ReadingStateContractError,
    );
    expect(() =>
      parseStoryReadingState({ ...validState(), snoozedUntil: "2026-08-30T12:00:00Z" }),
    ).toThrow(ReadingStateContractError);
    expect(() => parseStoryReadingState({ ...validState(), tagIds: [tagID, tagID] })).toThrow(
      ReadingStateContractError,
    );
  });

  it("fails closed on malformed annotation bodies", () => {
    expect(() =>
      parseAnnotationResponse({
        body: 7,
        createdAt: timestamp,
        endOffset: null,
        id: tagID,
        orphaned: false,
        quoteHash: null,
        revisionId: storyID,
        startOffset: null,
        storyId: storyID,
        type: "document_note",
        updatedAt: timestamp,
      }),
    ).toThrow(ReadingStateContractError);
  });

  it("parses bounded relevance feedback and rejects unknown signals", () => {
    expect(
      parseFeedbackResponse({
        createdAt: timestamp,
        id: tagID,
        note: null,
        storyId: storyID,
        type: "useful",
      }),
    ).toMatchObject({ storyId: storyID, type: "useful" });
    expect(() =>
      parseFeedbackResponse({
        createdAt: timestamp,
        id: tagID,
        note: null,
        storyId: storyID,
        type: "boost_source_trust",
      }),
    ).toThrow(ReadingStateContractError);
  });
});
