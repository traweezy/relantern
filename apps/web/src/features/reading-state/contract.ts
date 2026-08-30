import type {
  AnnotationType,
  BulkReadingMutation,
  ReadingCollection,
  ReadingLocation,
  ReadingMutation,
  StoryAnnotation,
  StoryFeedback,
  StoryFeedbackType,
  StoryReadingState,
  StoryTag,
} from "@relantern/domain";
import { parseStorySummary } from "../intelligence/contract";

export class ReadingStateContractError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "ReadingStateContractError";
  }
}

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

const recordValue = (value: unknown, name: string): Readonly<Record<string, unknown>> => {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new ReadingStateContractError(`${name} must be an object`);
  }
  return value as Readonly<Record<string, unknown>>;
};

const stringValue = (value: unknown, name: string, maximum = 20_000): string => {
  if (typeof value !== "string" || value.length === 0 || value.length > maximum) {
    throw new ReadingStateContractError(`${name} must be a bounded non-empty string`);
  }
  return value;
};

const boundedString = (value: unknown, name: string, maximum = 20_000): string => {
  if (typeof value !== "string" || value.length > maximum) {
    throw new ReadingStateContractError(`${name} must be a bounded string`);
  }
  return value;
};

const nullableString = (value: unknown, name: string, maximum = 20_000): string | null =>
  value === null ? null : stringValue(value, name, maximum);

const uuidValue = (value: unknown, name: string): string => {
  const identifier = stringValue(value, name, 36);
  if (!uuidPattern.test(identifier)) {
    throw new ReadingStateContractError(`${name} must be a UUID`);
  }
  return identifier;
};

const timestampValue = (value: unknown, name: string): string => {
  const timestamp = stringValue(value, name, 64);
  if (!Number.isFinite(Date.parse(timestamp))) {
    throw new ReadingStateContractError(`${name} must be an RFC 3339 timestamp`);
  }
  return timestamp;
};

const nullableTimestamp = (value: unknown, name: string): string | null =>
  value === null ? null : timestampValue(value, name);

const integerValue = (value: unknown, name: string, maximum = Number.MAX_SAFE_INTEGER): number => {
  if (!Number.isSafeInteger(value) || (value as number) < 0 || (value as number) > maximum) {
    throw new ReadingStateContractError(`${name} must be a bounded non-negative integer`);
  }
  return value as number;
};

const booleanValue = (value: unknown, name: string): boolean => {
  if (typeof value !== "boolean") {
    throw new ReadingStateContractError(`${name} must be a boolean`);
  }
  return value;
};

const enumValue = <T extends string>(value: unknown, name: string, allowed: ReadonlySet<T>): T => {
  if (typeof value !== "string" || !allowed.has(value as T)) {
    throw new ReadingStateContractError(`${name} contains an unsupported value`);
  }
  return value as T;
};

const locations = new Set(["inbox", "later", "archive"] as const);
const snoozeLocations = new Set(["inbox", "later"] as const);
const annotationTypes = new Set(["document_note", "highlight", "highlight_note"] as const);
const tagColors = new Set(["accent", "blue", "green", "orange", "purple", "red", "slate"] as const);
const feedbackTypes = new Set([
  "useful",
  "already_known",
  "irrelevant",
  "too_shallow",
  "too_verbose",
  "incorrect",
] as const);

export const parseStoryReadingState = (value: unknown, name = "state"): StoryReadingState => {
  const state = recordValue(value, name);
  const progress = state.readingProgress;
  if (typeof progress !== "number" || !Number.isFinite(progress) || progress < 0 || progress > 1) {
    throw new ReadingStateContractError(`${name}.readingProgress must be between zero and one`);
  }
  if (!Array.isArray(state.tagIds)) {
    throw new ReadingStateContractError(`${name}.tagIds must be an array`);
  }
  const location = enumValue<ReadingLocation>(state.location, `${name}.location`, locations);
  const snoozedFromLocation =
    state.snoozedFromLocation === null
      ? null
      : enumValue(state.snoozedFromLocation, `${name}.snoozedFromLocation`, snoozeLocations);
  if (snoozedFromLocation !== null && snoozedFromLocation !== location) {
    throw new ReadingStateContractError(`${name} has an inconsistent snooze location`);
  }
  const isRead = booleanValue(state.isRead, `${name}.isRead`);
  const readAt = nullableTimestamp(state.readAt, `${name}.readAt`);
  if (isRead !== (readAt !== null)) {
    throw new ReadingStateContractError(`${name} has an inconsistent read timestamp`);
  }
  const snoozedUntil = nullableTimestamp(state.snoozedUntil, `${name}.snoozedUntil`);
  if ((snoozedUntil === null) !== (snoozedFromLocation === null)) {
    throw new ReadingStateContractError(`${name} has an incomplete snooze state`);
  }
  const tagIds = state.tagIds.map((tagID, index) => uuidValue(tagID, `${name}.tagIds[${index}]`));
  if (new Set(tagIds).size !== tagIds.length) {
    throw new ReadingStateContractError(`${name}.tagIds contains duplicates`);
  }
  return {
    dismissedReason: nullableString(state.dismissedReason, `${name}.dismissedReason`, 80),
    isRead,
    lastParagraphId: nullableString(state.lastParagraphId, `${name}.lastParagraphId`, 255),
    laterPosition:
      state.laterPosition === null
        ? null
        : integerValue(state.laterPosition, `${name}.laterPosition`),
    location,
    readAt,
    readingProgress: progress,
    snoozedFromLocation,
    snoozedUntil,
    starredAt: nullableTimestamp(state.starredAt, `${name}.starredAt`),
    storyId: uuidValue(state.storyId, `${name}.storyId`),
    tagIds,
    updatedAt: timestampValue(state.updatedAt, `${name}.updatedAt`),
    version: integerValue(state.version, `${name}.version`),
  };
};

export const parseReadingCollection = (value: unknown): ReadingCollection => {
  const collection = recordValue(value, "collection");
  if (!Array.isArray(collection.items)) {
    throw new ReadingStateContractError("collection.items must be an array");
  }
  return {
    items: collection.items.map((entry, index) => {
      const item = recordValue(entry, `collection.items[${index}]`);
      return {
        state: parseStoryReadingState(item.state, `collection.items[${index}].state`),
        story: parseStorySummary(item.story, `collection.items[${index}].story`),
      };
    }),
    nextCursor:
      collection.nextCursor === null
        ? null
        : stringValue(collection.nextCursor, "collection.nextCursor", 512),
    total: integerValue(collection.total, "collection.total", 1_000_000),
  };
};

export const parseStoryReadingStates = (value: unknown): readonly StoryReadingState[] => {
  const response = recordValue(value, "storyStates");
  if (!Array.isArray(response.states)) {
    throw new ReadingStateContractError("storyStates.states must be an array");
  }
  return response.states.map((state, index) =>
    parseStoryReadingState(state, `storyStates.states[${index}]`),
  );
};

export const parseReadingMutation = (value: unknown, name = "mutation"): ReadingMutation => {
  const mutation = recordValue(value, name);
  return {
    mutationId: uuidValue(mutation.mutationId, `${name}.mutationId`),
    state: parseStoryReadingState(mutation.state, `${name}.state`),
    undoDeadline: timestampValue(mutation.undoDeadline, `${name}.undoDeadline`),
  };
};

export const parseBulkReadingMutation = (value: unknown): BulkReadingMutation => {
  const mutation = recordValue(value, "bulkMutation");
  if (!Array.isArray(mutation.mutations)) {
    throw new ReadingStateContractError("bulkMutation.mutations must be an array");
  }
  const mutations = mutation.mutations.map((entry, index) =>
    parseReadingMutation(entry, `bulkMutation.mutations[${index}]`),
  );
  const affectedCount = integerValue(mutation.affectedCount, "bulkMutation.affectedCount", 200);
  if (affectedCount !== mutations.length) {
    throw new ReadingStateContractError("bulkMutation affected count does not match its mutations");
  }
  return {
    affectedCount,
    bulkId: uuidValue(mutation.bulkId, "bulkMutation.bulkId"),
    mutations,
    undoDeadline: timestampValue(mutation.undoDeadline, "bulkMutation.undoDeadline"),
  };
};

const parseTag = (value: unknown, name: string): StoryTag => {
  const tag = recordValue(value, name);
  return {
    colorToken: enumValue(tag.colorToken, `${name}.colorToken`, tagColors),
    createdAt: timestampValue(tag.createdAt, `${name}.createdAt`),
    id: uuidValue(tag.id, `${name}.id`),
    name: stringValue(tag.name, `${name}.name`, 80),
    updatedAt: timestampValue(tag.updatedAt, `${name}.updatedAt`),
  };
};

export const parseTags = (value: unknown): readonly StoryTag[] => {
  const response = recordValue(value, "tagsResponse");
  if (!Array.isArray(response.tags)) {
    throw new ReadingStateContractError("tagsResponse.tags must be an array");
  }
  return response.tags.map((tag, index) => parseTag(tag, `tagsResponse.tags[${index}]`));
};

export const parseTagResponse = (value: unknown): StoryTag => parseTag(value, "tag");

export const parseFeedbackResponse = (value: unknown): StoryFeedback => {
  const feedback = recordValue(value, "feedback");
  return {
    createdAt: timestampValue(feedback.createdAt, "feedback.createdAt"),
    id: uuidValue(feedback.id, "feedback.id"),
    note: nullableString(feedback.note, "feedback.note", 2_000),
    storyId: uuidValue(feedback.storyId, "feedback.storyId"),
    type: enumValue<StoryFeedbackType>(feedback.type, "feedback.type", feedbackTypes),
  };
};

const parseAnnotation = (value: unknown, name: string): StoryAnnotation => {
  const annotation = recordValue(value, name);
  const quoteHash = nullableString(annotation.quoteHash, `${name}.quoteHash`, 64);
  if (quoteHash !== null && !/^[0-9a-f]{64}$/.test(quoteHash)) {
    throw new ReadingStateContractError(`${name}.quoteHash must be a SHA-256 digest`);
  }
  return {
    body: boundedString(annotation.body, `${name}.body`, 20_000),
    createdAt: timestampValue(annotation.createdAt, `${name}.createdAt`),
    endOffset:
      annotation.endOffset === null
        ? null
        : integerValue(annotation.endOffset, `${name}.endOffset`, 10_000_000),
    id: uuidValue(annotation.id, `${name}.id`),
    orphaned: booleanValue(annotation.orphaned, `${name}.orphaned`),
    quoteHash,
    revisionId: uuidValue(annotation.revisionId, `${name}.revisionId`),
    startOffset:
      annotation.startOffset === null
        ? null
        : integerValue(annotation.startOffset, `${name}.startOffset`, 10_000_000),
    storyId: uuidValue(annotation.storyId, `${name}.storyId`),
    type: enumValue<AnnotationType>(annotation.type, `${name}.type`, annotationTypes),
    updatedAt: timestampValue(annotation.updatedAt, `${name}.updatedAt`),
  };
};

export const parseAnnotations = (value: unknown): readonly StoryAnnotation[] => {
  const response = recordValue(value, "annotationsResponse");
  if (!Array.isArray(response.annotations)) {
    throw new ReadingStateContractError("annotationsResponse.annotations must be an array");
  }
  return response.annotations.map((annotation, index) =>
    parseAnnotation(annotation, `annotationsResponse.annotations[${index}]`),
  );
};

export const parseAnnotationResponse = (value: unknown): StoryAnnotation =>
  parseAnnotation(value, "annotation");
