import "server-only";

import type { ZodType } from "zod";
import { getOwnerSession, type OwnerSession } from "@/server/auth/session";
import { DiscoveryResponseError } from "./client";

const maximumRequestCharacters = 2_200_000;

export class DiscoveryRequestError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "DiscoveryRequestError";
  }
}

export const readDiscoveryJSON = async <T>(request: Request, schema: ZodType<T>): Promise<T> => {
  const encoded = await request.text();
  if (encoded.length === 0 || encoded.length > maximumRequestCharacters) {
    throw new DiscoveryRequestError("The request body is missing or too large.");
  }
  let decoded: unknown;
  try {
    decoded = JSON.parse(encoded);
  } catch {
    throw new DiscoveryRequestError("The request body is not valid JSON.");
  }
  const parsed = schema.safeParse(decoded);
  if (!parsed.success) {
    throw new DiscoveryRequestError("The request body does not match the discovery contract.");
  }
  return parsed.data;
};

const problem = (status: number, title: string, detail: string): Response =>
  Response.json(
    { detail, status, title, type: "about:blank" },
    { headers: { "cache-control": "no-store, private" }, status },
  );

const mapError = (error: unknown): Response => {
  if (error instanceof DiscoveryRequestError) {
    return problem(400, "Bad Request", error.message);
  }
  if (error instanceof DiscoveryResponseError) {
    switch (error.status) {
      case 400:
        return problem(400, "Bad Request", "The discovery request is invalid.");
      case 404:
        return problem(404, "Not Found", "The requested discovery resource does not exist.");
      case 409:
        return problem(409, "Conflict", "The preview changed or expired; create a new preview.");
    }
  }
  return problem(502, "Bad Gateway", "The private discovery service is unavailable.");
};

export const ownerDiscoveryJSONRoute = async <T>(
  operation: (owner: OwnerSession) => Promise<T>,
): Promise<Response> => {
  const owner = await getOwnerSession();
  if (owner === null) {
    return problem(401, "Unauthorized", "An authenticated owner session is required.");
  }
  try {
    return Response.json(await operation(owner), {
      headers: { "cache-control": "no-store, private" },
    });
  } catch (error: unknown) {
    return mapError(error);
  }
};

export const ownerDiscoveryFileRoute = async (
  operation: (owner: OwnerSession) => Promise<Response>,
): Promise<Response> => {
  const owner = await getOwnerSession();
  if (owner === null) {
    return problem(401, "Unauthorized", "An authenticated owner session is required.");
  }
  try {
    const upstream = await operation(owner);
    return new Response(upstream.body, {
      headers: {
        "cache-control": "no-store, private",
        "content-disposition":
          upstream.headers.get("content-disposition") ?? 'attachment; filename="relantern-export"',
        "content-type": upstream.headers.get("content-type") ?? "application/octet-stream",
        "x-content-type-options": "nosniff",
      },
      status: upstream.status,
    });
  } catch (error: unknown) {
    return mapError(error);
  }
};
