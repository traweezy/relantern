import "server-only";

import type { LiveSnapshot, StoryDetail, TodaySnapshot } from "@relantern/domain";
import {
  parseLiveSnapshot,
  parseStoryDetail,
  parseTodaySnapshot,
} from "@/features/intelligence/contract";
import { getIntelligenceAPIConfiguration } from "./config";

const maximumResponseCharacters = 2_000_000;

export class IntelligenceResponseError extends Error {
  public readonly status: number;

  public constructor(message: string, status: number) {
    super(message);
    this.name = "IntelligenceResponseError";
    this.status = status;
  }
}

const request = async <T>(path: string, parse: (value: unknown) => T): Promise<T> => {
  const configuration = getIntelligenceAPIConfiguration();
  const response = await fetch(new URL(path, configuration.baseURL), {
    cache: "no-store",
    headers: {
      accept: "application/json",
      authorization: `Bearer ${configuration.serviceToken}`,
    },
    signal: AbortSignal.timeout(5_000),
  });
  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.toLowerCase().startsWith("application/json")) {
    throw new IntelligenceResponseError("The private API returned an unsupported response.", 502);
  }
  const encoded = await response.text();
  if (encoded.length > maximumResponseCharacters) {
    throw new IntelligenceResponseError("The private API response exceeded its size limit.", 502);
  }
  if (!response.ok) {
    throw new IntelligenceResponseError("The private API request failed.", response.status);
  }
  let decoded: unknown;
  try {
    decoded = JSON.parse(encoded);
  } catch {
    throw new IntelligenceResponseError("The private API returned invalid JSON.", 502);
  }
  return parse(decoded);
};

export const getTodaySnapshot = (): Promise<TodaySnapshot> =>
  request("/api/v1/today", parseTodaySnapshot);

export const getLiveSnapshot = (): Promise<LiveSnapshot> =>
  request("/api/v1/live", parseLiveSnapshot);

export const getStory = (storyID: string): Promise<StoryDetail> =>
  request(`/api/v1/stories/${encodeURIComponent(storyID)}`, parseStoryDetail);
