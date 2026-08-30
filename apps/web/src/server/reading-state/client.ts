import "server-only";

import type {
  BulkReadingMutation,
  ReadingCollection,
  ReadingMutation,
  StoryAnnotation,
  StoryFeedback,
  StoryReadingState,
  StoryTag,
} from "@relantern/domain";
import type {
  AnnotationInput,
  AnnotationUpdate,
  BulkReadingMutationCommand,
  FeedbackCommand,
  LaterOrderCommand,
  ReadingMutationCommand,
  TagInput,
} from "@/features/reading-state/commands";
import {
  parseAnnotationResponse,
  parseAnnotations,
  parseBulkReadingMutation,
  parseFeedbackResponse,
  parseReadingCollection,
  parseReadingMutation,
  parseStoryReadingState,
  parseStoryReadingStates,
  parseTagResponse,
  parseTags,
} from "@/features/reading-state/contract";
import { getIntelligenceAPIConfiguration } from "@/server/intelligence/config";

const maximumResponseCharacters = 2_000_000;

export class ReadingStateResponseError extends Error {
  public readonly status: number;

  public constructor(message: string, status: number) {
    super(message);
    this.name = "ReadingStateResponseError";
    this.status = status;
  }
}

const request = async <T>(
  path: string,
  userID: string,
  parse: (value: unknown) => T,
  method = "GET",
  body?: unknown,
): Promise<T> => {
  const configuration = getIntelligenceAPIConfiguration();
  const response = await fetch(new URL(path, configuration.baseURL), {
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    cache: "no-store",
    headers: {
      accept: "application/json",
      authorization: `Bearer ${configuration.serviceToken}`,
      ...(body === undefined ? {} : { "content-type": "application/json" }),
      "x-relantern-user-id": userID,
    },
    method,
    signal: AbortSignal.timeout(5_000),
  });
  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.toLowerCase().startsWith("application/json")) {
    throw new ReadingStateResponseError("The private API returned an unsupported response.", 502);
  }
  const encoded = await response.text();
  if (encoded.length > maximumResponseCharacters) {
    throw new ReadingStateResponseError("The private API response exceeded its size limit.", 502);
  }
  if (!response.ok) {
    throw new ReadingStateResponseError(
      "The private reading-state request failed.",
      response.status,
    );
  }
  let decoded: unknown;
  try {
    decoded = JSON.parse(encoded);
  } catch {
    throw new ReadingStateResponseError("The private API returned invalid JSON.", 502);
  }
  return parse(decoded);
};

export const getReadingCollection = (
  kind: "archive" | "inbox" | "later" | "snoozed" | "starred",
  userID: string,
  cursor?: string,
): Promise<ReadingCollection> => {
  const parameters = new URLSearchParams({ limit: "50" });
  if (cursor !== undefined) {
    parameters.set("cursor", cursor);
  }
  return request(`/api/v1/${kind}?${parameters.toString()}`, userID, parseReadingCollection);
};

export const getStoryState = (userID: string, storyID: string): Promise<StoryReadingState> =>
  request(`/api/v1/stories/${encodeURIComponent(storyID)}/state`, userID, parseStoryReadingState);

export const mutateStoryState = (
  userID: string,
  storyID: string,
  command: ReadingMutationCommand,
): Promise<ReadingMutation> =>
  request(
    `/api/v1/stories/${encodeURIComponent(storyID)}/state`,
    userID,
    parseReadingMutation,
    "PATCH",
    command,
  );

export const getStoryStates = (
  userID: string,
  storyIDs: readonly string[],
): Promise<readonly StoryReadingState[]> =>
  request("/api/v1/stories/state-query", userID, parseStoryReadingStates, "POST", {
    storyIds: storyIDs,
  });

export const undoStoryState = (
  userID: string,
  storyID: string,
  mutationID: string,
): Promise<ReadingMutation> =>
  request(
    `/api/v1/stories/${encodeURIComponent(storyID)}/undo`,
    userID,
    parseReadingMutation,
    "POST",
    { mutationId: mutationID },
  );

export const bulkMutateStoryState = (
  userID: string,
  command: BulkReadingMutationCommand,
): Promise<BulkReadingMutation> =>
  request("/api/v1/stories/bulk-state", userID, parseBulkReadingMutation, "POST", command);

export const undoBulkStoryState = (userID: string, bulkID: string): Promise<BulkReadingMutation> =>
  request("/api/v1/stories/bulk-state/undo", userID, parseBulkReadingMutation, "POST", {
    bulkId: bulkID,
  });

export const reorderLater = (
  userID: string,
  command: LaterOrderCommand,
): Promise<BulkReadingMutation> =>
  request("/api/v1/later/order", userID, parseBulkReadingMutation, "PUT", command);

export const recordStoryFeedback = (
  userID: string,
  storyID: string,
  command: FeedbackCommand,
): Promise<StoryFeedback> =>
  request(
    `/api/v1/stories/${encodeURIComponent(storyID)}/feedback`,
    userID,
    parseFeedbackResponse,
    "POST",
    command,
  );

export const getTags = (userID: string): Promise<readonly StoryTag[]> =>
  request("/api/v1/tags", userID, parseTags);

export const createTag = (userID: string, input: TagInput): Promise<StoryTag> =>
  request("/api/v1/tags", userID, parseTagResponse, "POST", input);

export const updateTag = (userID: string, tagID: string, input: TagInput): Promise<StoryTag> =>
  request(`/api/v1/tags/${encodeURIComponent(tagID)}`, userID, parseTagResponse, "PATCH", input);

export const deleteTag = async (userID: string, tagID: string): Promise<void> => {
  await request(`/api/v1/tags/${encodeURIComponent(tagID)}`, userID, () => undefined, "DELETE");
};

export const getAnnotations = (
  userID: string,
  storyID: string,
): Promise<readonly StoryAnnotation[]> =>
  request(`/api/v1/stories/${encodeURIComponent(storyID)}/annotations`, userID, parseAnnotations);

export const createAnnotation = (
  userID: string,
  storyID: string,
  input: AnnotationInput,
): Promise<StoryAnnotation> =>
  request(
    `/api/v1/stories/${encodeURIComponent(storyID)}/annotations`,
    userID,
    parseAnnotationResponse,
    "POST",
    input,
  );

export const updateAnnotation = (
  userID: string,
  annotationID: string,
  input: AnnotationUpdate,
): Promise<StoryAnnotation> =>
  request(
    `/api/v1/annotations/${encodeURIComponent(annotationID)}`,
    userID,
    parseAnnotationResponse,
    "PATCH",
    input,
  );

export const deleteAnnotation = async (userID: string, annotationID: string): Promise<void> => {
  await request(
    `/api/v1/annotations/${encodeURIComponent(annotationID)}`,
    userID,
    () => undefined,
    "DELETE",
  );
};
