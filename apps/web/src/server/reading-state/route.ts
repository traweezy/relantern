import "server-only";

import type { ZodType } from "zod";
import { getOwnerSession, type OwnerSession } from "@/server/auth/session";
import { ReadingStateResponseError } from "./client";

const maximumRequestCharacters = 65_536;

export class ReadingStateRequestError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "ReadingStateRequestError";
  }
}

export const readValidatedJSON = async <T>(request: Request, schema: ZodType<T>): Promise<T> => {
  const encoded = await request.text();
  if (encoded.length === 0 || encoded.length > maximumRequestCharacters) {
    throw new ReadingStateRequestError("The request body is missing or too large.");
  }
  let decoded: unknown;
  try {
    decoded = JSON.parse(encoded);
  } catch {
    throw new ReadingStateRequestError("The request body is not valid JSON.");
  }
  const parsed = schema.safeParse(decoded);
  if (!parsed.success) {
    throw new ReadingStateRequestError("The request body does not match the command contract.");
  }
  return parsed.data;
};

const problem = (status: number, title: string, detail: string): Response =>
  Response.json(
    { detail, status, title, type: "about:blank" },
    { headers: { "cache-control": "no-store, private" }, status },
  );

export const ownerJSONRoute = async <T>(
  operation: (owner: OwnerSession) => Promise<T>,
): Promise<Response> => {
  const owner = await getOwnerSession();
  if (owner === null) {
    return problem(401, "Unauthorized", "An authenticated owner session is required.");
  }
  try {
    const body = await operation(owner);
    return Response.json(body, { headers: { "cache-control": "no-store, private" } });
  } catch (error: unknown) {
    if (error instanceof ReadingStateRequestError) {
      return problem(400, "Bad Request", error.message);
    }
    if (error instanceof ReadingStateResponseError) {
      switch (error.status) {
        case 400:
          return problem(400, "Bad Request", "The reading-state command is invalid.");
        case 404:
          return problem(404, "Not Found", "The requested owner resource does not exist.");
        case 409:
          return problem(409, "Conflict", "This story changed. Refresh before retrying.");
        case 410:
          return problem(410, "Gone", "The ten-second Undo window has expired.");
      }
    }
    return problem(502, "Bad Gateway", "The private reading-state service is unavailable.");
  }
};
