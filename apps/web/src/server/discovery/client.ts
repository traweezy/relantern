import "server-only";

import type {
  ImportCommitResult,
  ImportPreview,
  IntelligenceSearchResponse,
  ManualCapture,
  ReleaseCatalog,
  SavedIntelligenceSearch,
} from "@relantern/domain";
import type {
  ManualCaptureCommand,
  MarkdownExportCommand,
  OPMLCommitCommand,
  OPMLPreviewCommand,
  SaveSearchCommand,
} from "@/features/discovery/commands";
import {
  parseImportCommit,
  parseImportPreview,
  parseManualCapture,
  parseReleaseCatalog,
  parseSavedSearch,
  parseSavedSearches,
  parseSearchResponse,
} from "@/features/discovery/contract";
import { getIntelligenceAPIConfiguration } from "@/server/intelligence/config";

const maximumResponseCharacters = 2_000_000;

export class DiscoveryResponseError extends Error {
  public readonly status: number;

  public constructor(message: string, status: number) {
    super(message);
    this.name = "DiscoveryResponseError";
    this.status = status;
  }
}

const internalRequest = async (
  path: string,
  userID: string,
  init: Readonly<{ body?: unknown; method?: string }> = {},
): Promise<Response> => {
  const configuration = getIntelligenceAPIConfiguration();
  return fetch(new URL(path, configuration.baseURL), {
    ...(init.body === undefined ? {} : { body: JSON.stringify(init.body) }),
    cache: "no-store",
    headers: {
      accept: "application/json",
      authorization: `Bearer ${configuration.serviceToken}`,
      ...(init.body === undefined ? {} : { "content-type": "application/json" }),
      "x-relantern-user-id": userID,
    },
    method: init.method ?? "GET",
    signal: AbortSignal.timeout(10_000),
  });
};

const requestJSON = async <T>(
  path: string,
  userID: string,
  parse: (value: unknown) => T,
  init: Readonly<{ body?: unknown; method?: string }> = {},
): Promise<T> => {
  const response = await internalRequest(path, userID, init);
  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.toLowerCase().startsWith("application/json")) {
    throw new DiscoveryResponseError("The private API returned an unsupported response.", 502);
  }
  const encoded = await response.text();
  if (encoded.length > maximumResponseCharacters) {
    throw new DiscoveryResponseError("The private API response exceeded its size limit.", 502);
  }
  if (!response.ok) {
    throw new DiscoveryResponseError("The private discovery request failed.", response.status);
  }
  let decoded: unknown;
  try {
    decoded = JSON.parse(encoded);
  } catch {
    throw new DiscoveryResponseError("The private API returned invalid JSON.", 502);
  }
  return parse(decoded);
};

export type SearchParameters = Readonly<{
  action?: string;
  after?: string;
  before?: string;
  lifecycle?: string;
  q: string;
  radar?: string;
  saved?: string;
  sourceTier?: string;
  topic?: string;
}>;

export const searchIntelligence = (
  userID: string,
  parameters: SearchParameters,
): Promise<IntelligenceSearchResponse> => {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(parameters)) {
    if (value !== undefined && value !== "") {
      query.set(key, value);
    }
  }
  return requestJSON(`/api/v1/search?${query.toString()}`, userID, parseSearchResponse);
};

export const getSavedSearches = (userID: string): Promise<readonly SavedIntelligenceSearch[]> =>
  requestJSON("/api/v1/searches", userID, parseSavedSearches);

export const saveSearch = (
  userID: string,
  command: SaveSearchCommand,
): Promise<SavedIntelligenceSearch> =>
  requestJSON("/api/v1/searches", userID, parseSavedSearch, { body: command, method: "POST" });

export const deleteSavedSearch = async (userID: string, savedSearchID: string): Promise<void> => {
  const response = await internalRequest(
    `/api/v1/searches/${encodeURIComponent(savedSearchID)}`,
    userID,
    { method: "DELETE" },
  );
  if (!response.ok) {
    throw new DiscoveryResponseError("The saved search could not be deleted.", response.status);
  }
};

export const getReleaseCatalog = (userID: string): Promise<ReleaseCatalog> =>
  requestJSON("/api/v1/releases", userID, parseReleaseCatalog);

export const importInboxURL = (
  userID: string,
  command: ManualCaptureCommand,
): Promise<ManualCapture> =>
  requestJSON("/api/v1/inbox/import-url", userID, parseManualCapture, {
    body: command,
    method: "POST",
  });

export const previewOPML = (userID: string, command: OPMLPreviewCommand): Promise<ImportPreview> =>
  requestJSON("/api/v1/imports/opml/preview", userID, parseImportPreview, {
    body: command,
    method: "POST",
  });

export const commitOPML = (
  userID: string,
  command: OPMLCommitCommand,
): Promise<ImportCommitResult> =>
  requestJSON("/api/v1/imports/opml/commit", userID, parseImportCommit, {
    body: command,
    method: "POST",
  });

export const exportDiscoveryFile = async (
  userID: string,
  path: string,
  init: Readonly<{ body?: MarkdownExportCommand; method?: string }> = {},
): Promise<Response> => {
  const response = await internalRequest(path, userID, init);
  if (!response.ok) {
    throw new DiscoveryResponseError("The private export request failed.", response.status);
  }
  return response;
};
