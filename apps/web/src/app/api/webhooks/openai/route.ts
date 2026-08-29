import { readFile } from "node:fs/promises";
import OpenAI from "openai";
import { getIntelligenceAPIConfiguration } from "../../../../server/intelligence/config";

const maximumBodyBytes = 16 * 1024;
const forwardingTimeoutMs = 5_000;
const acceptedEventTypes = new Set([
  "response.completed",
  "response.failed",
  "response.incomplete",
  "response.cancelled",
]);

type ResponseEvent = {
  readonly id: string;
  readonly type: string;
  readonly created_at: number;
  readonly data: { readonly id: string };
};

const readSecret = async (valueName: string, fileName: string): Promise<string> => {
  const direct = process.env[valueName]?.trim();
  if (direct) {
    return direct;
  }
  const path = process.env[fileName]?.trim();
  if (!path) {
    throw new Error(`${valueName} or ${fileName} is required`);
  }
  const value = (await readFile(path, "utf8")).trim();
  if (!value) {
    throw new Error(`${fileName} is empty`);
  }
  return value;
};

const readRawBody = async (request: Request): Promise<string> => {
  const declaredLength = request.headers.get("content-length");
  if (declaredLength !== null) {
    const parsedLength = Number.parseInt(declaredLength, 10);
    if (
      !Number.isSafeInteger(parsedLength) ||
      parsedLength < 0 ||
      parsedLength > maximumBodyBytes
    ) {
      throw new RangeError("webhook body exceeds the configured limit");
    }
  }
  if (request.body === null) {
    throw new TypeError("webhook body is required");
  }
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let totalBytes = 0;
  while (true) {
    const result = await reader.read();
    if (result.done) {
      break;
    }
    totalBytes += result.value.byteLength;
    if (totalBytes > maximumBodyBytes) {
      await reader.cancel("webhook body exceeds the configured limit");
      throw new RangeError("webhook body exceeds the configured limit");
    }
    chunks.push(result.value);
  }
  const payload = new Uint8Array(totalBytes);
  let offset = 0;
  for (const chunk of chunks) {
    payload.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder("utf-8", { fatal: true }).decode(payload);
};

const privateAPIURL = (baseURL: string): URL => new URL("/internal/v1/openai/events", baseURL);

const isResponseEvent = (event: unknown): event is ResponseEvent => {
  if (typeof event !== "object" || event === null) {
    return false;
  }
  const candidate = event as Partial<ResponseEvent>;
  return (
    typeof candidate.id === "string" &&
    candidate.id.length > 0 &&
    candidate.id.length <= 255 &&
    typeof candidate.type === "string" &&
    acceptedEventTypes.has(candidate.type) &&
    typeof candidate.created_at === "number" &&
    Number.isSafeInteger(candidate.created_at) &&
    typeof candidate.data === "object" &&
    candidate.data !== null &&
    typeof candidate.data.id === "string" &&
    candidate.data.id.length > 0 &&
    candidate.data.id.length <= 255
  );
};

export const POST = async (request: Request): Promise<Response> => {
  let rawBody: string;
  try {
    rawBody = await readRawBody(request);
  } catch {
    return Response.json({ error: "invalid_request" }, { status: 413 });
  }

  let event: unknown;
  let webhookID: string;
  try {
    const webhookSecret = await readSecret("OPENAI_WEBHOOK_SECRET", "OPENAI_WEBHOOK_SECRET_FILE");
    const client = new OpenAI({
      apiKey: "unused-webhook-verifier",
      webhookSecret,
    });
    event = await client.webhooks.unwrap(rawBody, request.headers);
    webhookID = request.headers.get("webhook-id") ?? "";
  } catch {
    return Response.json({ error: "invalid_signature" }, { status: 400 });
  }

  if (!isResponseEvent(event) || webhookID.length === 0 || webhookID.length > 255) {
    return Response.json({ error: "unsupported_event" }, { status: 400 });
  }

  try {
    const configuration = getIntelligenceAPIConfiguration();
    const forwarded = await fetch(privateAPIURL(configuration.baseURL), {
      method: "POST",
      headers: {
        authorization: `Bearer ${configuration.serviceToken}`,
        "content-type": "application/json",
      },
      body: JSON.stringify({
        webhookId: webhookID,
        eventId: event.id,
        eventType: event.type,
        responseId: event.data.id,
        eventCreatedAt: new Date(event.created_at * 1_000).toISOString(),
      }),
      cache: "no-store",
      redirect: "manual",
      signal: AbortSignal.timeout(forwardingTimeoutMs),
    });
    if (forwarded.status !== 202) {
      return Response.json({ error: "forwarding_failed" }, { status: 502 });
    }
  } catch {
    return Response.json({ error: "forwarding_failed" }, { status: 502 });
  }

  return Response.json({ accepted: true }, { status: 202 });
};
