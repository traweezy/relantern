import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("server-only", () => ({}));

import { POST } from "./route";

const webhookSecret = "0123456789abcdef0123456789abcdef";
const serviceToken = "abcdef0123456789abcdef0123456789";

const signedRequest = async (payload: string, signaturePayload = payload): Promise<Request> => {
  const webhookID = "wh_test_123";
  const timestamp = Math.floor(Date.now() / 1_000).toString();
  const key = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(webhookSecret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const signature = await crypto.subtle.sign(
    "HMAC",
    key,
    new TextEncoder().encode(`${webhookID}.${timestamp}.${signaturePayload}`),
  );
  return new Request("http://localhost/api/webhooks/openai", {
    method: "POST",
    body: payload,
    headers: {
      "content-type": "application/json",
      "webhook-id": webhookID,
      "webhook-signature": `v1,${Buffer.from(signature).toString("base64")}`,
      "webhook-timestamp": timestamp,
    },
  });
};

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("OpenAI webhook route", () => {
  it("verifies the untouched body and forwards only the bounded event envelope", async () => {
    vi.stubEnv("OPENAI_WEBHOOK_SECRET", webhookSecret);
    vi.stubEnv("WEB_INTERNAL_SERVICE_TOKEN", serviceToken);
    vi.stubEnv("INTERNAL_API_URL", "http://api:8080");
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 202 }));
    vi.stubGlobal("fetch", fetchMock);
    const payload = JSON.stringify({
      id: "evt_test_123",
      object: "event",
      type: "response.completed",
      created_at: Math.floor(Date.now() / 1_000),
      data: { id: "resp_test_123" },
    });

    const response = await POST(await signedRequest(payload));

    expect(response.status).toBe(202);
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(url?.toString()).toBe("http://api:8080/internal/v1/openai/events");
    expect(init?.headers).toEqual({
      authorization: `Bearer ${serviceToken}`,
      "content-type": "application/json",
    });
    expect(JSON.parse(String(init?.body))).toMatchObject({
      webhookId: "wh_test_123",
      eventId: "evt_test_123",
      eventType: "response.completed",
      responseId: "resp_test_123",
    });
  });

  it("rejects a body changed after signing without forwarding", async () => {
    vi.stubEnv("OPENAI_WEBHOOK_SECRET", webhookSecret);
    vi.stubEnv("WEB_INTERNAL_SERVICE_TOKEN", serviceToken);
    vi.stubEnv("INTERNAL_API_URL", "http://api:8080");
    const fetchMock = vi.fn<typeof fetch>();
    vi.stubGlobal("fetch", fetchMock);
    const signedPayload = JSON.stringify({
      id: "evt_test_123",
      type: "response.completed",
      created_at: Math.floor(Date.now() / 1_000),
      data: { id: "resp_test_123" },
    });
    const changedPayload = signedPayload.replace("resp_test_123", "resp_changed");

    const response = await POST(await signedRequest(changedPayload, signedPayload));

    expect(response.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects verified non-response events", async () => {
    vi.stubEnv("OPENAI_WEBHOOK_SECRET", webhookSecret);
    vi.stubEnv("WEB_INTERNAL_SERVICE_TOKEN", serviceToken);
    vi.stubEnv("INTERNAL_API_URL", "http://api:8080");
    const fetchMock = vi.fn<typeof fetch>();
    vi.stubGlobal("fetch", fetchMock);
    const payload = JSON.stringify({
      id: "evt_batch_123",
      type: "batch.completed",
      created_at: Math.floor(Date.now() / 1_000),
      data: { id: "batch_123" },
    });

    const response = await POST(await signedRequest(payload));

    expect(response.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
