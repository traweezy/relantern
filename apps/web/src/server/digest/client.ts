import "server-only";

import type { DigestRecord, DigestSnapshot } from "@relantern/domain";
import { parseDigestRecord, parseDigestSnapshot } from "@/features/digest/contract";
import { ControlPlaneResponseError } from "@/server/controlplane/client";
import { getIntelligenceAPIConfiguration } from "@/server/intelligence/config";

const maximumResponseCharacters = 2_000_000;

const request = async <T>(
  path: string,
  userID: string,
  parse: (value: unknown) => T,
  method = "GET",
): Promise<T> => {
  const configuration = getIntelligenceAPIConfiguration();
  const response = await fetch(new URL(path, configuration.baseURL), {
    cache: "no-store",
    headers: {
      accept: "application/json",
      authorization: `Bearer ${configuration.serviceToken}`,
      "x-relantern-user-id": userID,
    },
    method,
    signal: AbortSignal.timeout(10_000),
  });
  const encoded = await response.text();
  if (
    !response.headers.get("content-type")?.toLowerCase().startsWith("application/json") ||
    encoded.length > maximumResponseCharacters
  ) {
    throw new ControlPlaneResponseError(
      "The private digest API returned an invalid response.",
      502,
    );
  }
  if (!response.ok) {
    throw new ControlPlaneResponseError("The private digest request failed.", response.status);
  }
  return parse(JSON.parse(encoded) as unknown);
};

export const getDigests = (userID: string): Promise<DigestSnapshot> =>
  request("/api/v1/digests", userID, parseDigestSnapshot);

export const retryDigestDelivery = (userID: string, digestID: string): Promise<DigestRecord> =>
  request(
    `/api/v1/digests/${encodeURIComponent(digestID)}/retry-delivery`,
    userID,
    parseDigestRecord,
    "POST",
  );
