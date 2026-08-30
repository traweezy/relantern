import "server-only";

import type {
  ManagedSource,
  OperationsSnapshot,
  ScheduleActionResult,
  ScheduleDefinition,
  SchedulePreview,
  SettingsSnapshot,
  SourcePreference,
  SourcesSnapshot,
} from "@relantern/domain";
import type {
  ScheduleActionCommand,
  ScheduleCommand,
  SettingsCommand,
  SourceActionCommand,
  SourcePreferenceCommand,
} from "@/features/control-plane/commands";
import {
  parseManagedSource,
  parseOperationsSnapshot,
  parseScheduleActionResult,
  parseScheduleDefinition,
  parseSchedulePreview,
  parseSettingsSnapshot,
  parseSourcePreference,
  parseSourcesSnapshot,
} from "@/features/control-plane/contract";
import { getIntelligenceAPIConfiguration } from "@/server/intelligence/config";

const maximumResponseCharacters = 3_000_000;

export class ControlPlaneResponseError extends Error {
  public readonly status: number;

  public constructor(message: string, status: number) {
    super(message);
    this.name = "ControlPlaneResponseError";
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
    throw new ControlPlaneResponseError("The private API returned an unsupported response.", 502);
  }
  const encoded = await response.text();
  if (encoded.length > maximumResponseCharacters) {
    throw new ControlPlaneResponseError("The private API response exceeded its size limit.", 502);
  }
  if (!response.ok) {
    throw new ControlPlaneResponseError(
      "The private control-plane request failed.",
      response.status,
    );
  }
  let decoded: unknown;
  try {
    decoded = JSON.parse(encoded);
  } catch {
    throw new ControlPlaneResponseError("The private API returned invalid JSON.", 502);
  }
  return parse(decoded);
};

export const getSources = (userID: string): Promise<SourcesSnapshot> =>
  requestJSON("/api/v1/sources", userID, parseSourcesSnapshot);

export const updateSourcePreference = (
  userID: string,
  sourceID: string,
  command: SourcePreferenceCommand,
): Promise<SourcePreference> =>
  requestJSON(
    `/api/v1/sources/${encodeURIComponent(sourceID)}/preference`,
    userID,
    parseSourcePreference,
    { body: command, method: "PUT" },
  );

export const actOnSource = (
  userID: string,
  sourceID: string,
  command: SourceActionCommand,
): Promise<ManagedSource> =>
  requestJSON(
    `/api/v1/sources/${encodeURIComponent(sourceID)}/actions`,
    userID,
    parseManagedSource,
    { body: command, method: "POST" },
  );

export const getSettings = (userID: string): Promise<SettingsSnapshot> =>
  requestJSON("/api/v1/settings", userID, parseSettingsSnapshot);

export const updateSettings = (
  userID: string,
  command: SettingsCommand,
): Promise<SettingsSnapshot> =>
  requestJSON("/api/v1/settings", userID, parseSettingsSnapshot, {
    body: command,
    method: "PUT",
  });

export const updateSchedule = (
  userID: string,
  scheduleID: string,
  command: ScheduleCommand,
): Promise<ScheduleDefinition> =>
  requestJSON(
    `/api/v1/settings/schedules/${encodeURIComponent(scheduleID)}`,
    userID,
    parseScheduleDefinition,
    { body: command, method: "PUT" },
  );

export const actOnSchedule = (
  userID: string,
  scheduleID: string,
  command: ScheduleActionCommand,
): Promise<ScheduleActionResult> =>
  requestJSON(
    `/api/v1/settings/schedules/${encodeURIComponent(scheduleID)}/actions`,
    userID,
    parseScheduleActionResult,
    { body: command, method: "POST" },
  );

export const previewSchedule = (userID: string, scheduleID: string): Promise<SchedulePreview> =>
  requestJSON(
    `/api/v1/settings/schedules/${encodeURIComponent(scheduleID)}/preview`,
    userID,
    parseSchedulePreview,
  );

export const getOperations = (userID: string): Promise<OperationsSnapshot> =>
  requestJSON("/api/v1/operations", userID, parseOperationsSnapshot);
