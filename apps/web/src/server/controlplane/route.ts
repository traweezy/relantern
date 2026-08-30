import "server-only";

import type { ZodType } from "zod";
import { getOwnerSession, type OwnerSession } from "@/server/auth/session";
import { ControlPlaneResponseError } from "./client";

const maximumRequestCharacters = 300_000;

export class ControlPlaneRequestError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "ControlPlaneRequestError";
  }
}

export const readControlPlaneJSON = async <T>(request: Request, schema: ZodType<T>): Promise<T> => {
  const encoded = await request.text();
  if (encoded.length === 0 || encoded.length > maximumRequestCharacters) {
    throw new ControlPlaneRequestError("The request body is missing or too large.");
  }
  let decoded: unknown;
  try {
    decoded = JSON.parse(encoded);
  } catch {
    throw new ControlPlaneRequestError("The request body is not valid JSON.");
  }
  const parsed = schema.safeParse(decoded);
  if (!parsed.success) {
    throw new ControlPlaneRequestError(
      "The request body does not match the control-plane contract.",
    );
  }
  return parsed.data;
};

const problem = (status: number, title: string, detail: string): Response =>
  Response.json(
    { detail, status, title, type: "about:blank" },
    { headers: { "cache-control": "no-store, private" }, status },
  );

const mapError = (error: unknown): Response => {
  if (error instanceof ControlPlaneRequestError) {
    return problem(400, "Bad Request", error.message);
  }
  if (error instanceof ControlPlaneResponseError) {
    switch (error.status) {
      case 400:
        return problem(400, "Bad Request", "The control-plane request is invalid.");
      case 404:
        return problem(404, "Not Found", "The requested private resource does not exist.");
      case 409:
        return problem(
          409,
          "Conflict",
          "The resource changed or cannot enter that state; refresh and try again.",
        );
    }
  }
  return problem(502, "Bad Gateway", "The private control-plane service is unavailable.");
};

export const ownerControlPlaneJSONRoute = async <T>(
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
