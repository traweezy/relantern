import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("server-only", () => ({}));
vi.mock("@/server/auth/session", () => ({ getOwnerSession: vi.fn() }));
vi.mock("@/server/intelligence/config", () => ({ getIntelligenceAPIConfiguration: vi.fn() }));

import { getOwnerSession } from "@/server/auth/session";
import { getIntelligenceAPIConfiguration } from "@/server/intelligence/config";
import { GET } from "./route";

const token = "0123456789abcdef0123456789abcdef";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe("authenticated live stream proxy", () => {
  it("rejects visitors before opening the private stream", async () => {
    vi.mocked(getOwnerSession).mockResolvedValue(null);
    const fetchMock = vi.fn<typeof fetch>();
    vi.stubGlobal("fetch", fetchMock);

    const response = await GET(new Request("http://localhost/api/intelligence/live"));

    expect(response.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("forwards a bounded cursor and keeps the credential server side", async () => {
    vi.mocked(getOwnerSession).mockResolvedValue({
      displayName: "Owner",
      expiresAt: new Date(Date.now() + 60_000),
      login: "owner",
      timezone: "UTC",
      userID: "01991234-5678-7abc-8def-0123456789ab",
    });
    vi.mocked(getIntelligenceAPIConfiguration).mockReturnValue({
      baseURL: "http://api:8080",
      serviceToken: token,
    });
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      new Response("id: 42\nevent: story-created\ndata: {}\n\n", {
        headers: { "content-type": "text/event-stream" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const response = await GET(new Request("http://localhost/api/intelligence/live?after=41"));

    expect(response.status).toBe(200);
    expect(await response.text()).toContain("id: 42");
    const [target, init] = fetchMock.mock.calls[0] ?? [];
    expect(target?.toString()).toBe("http://api:8080/internal/v1/live/stream?after=41");
    expect(init?.headers).toMatchObject({ authorization: `Bearer ${token}` });
    expect(response.headers.get("authorization")).toBeNull();
  });

  it("rejects malformed cursors before forwarding", async () => {
    vi.mocked(getOwnerSession).mockResolvedValue({
      displayName: "Owner",
      expiresAt: new Date(Date.now() + 60_000),
      login: "owner",
      timezone: "UTC",
      userID: "01991234-5678-7abc-8def-0123456789ab",
    });
    const fetchMock = vi.fn<typeof fetch>();
    vi.stubGlobal("fetch", fetchMock);
    const response = await GET(new Request("http://localhost/api/intelligence/live?after=1x"));
    expect(response.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("returns a problem response when the private API configuration is missing", async () => {
    vi.mocked(getOwnerSession).mockResolvedValue({
      displayName: "Owner",
      expiresAt: new Date(Date.now() + 60_000),
      login: "owner",
      timezone: "UTC",
      userID: "01991234-5678-7abc-8def-0123456789ab",
    });
    vi.mocked(getIntelligenceAPIConfiguration).mockImplementation(() => {
      throw new Error("missing private token");
    });
    const fetchMock = vi.fn<typeof fetch>();
    vi.stubGlobal("fetch", fetchMock);

    const response = await GET(new Request("http://localhost/api/intelligence/live"));

    expect(response.status).toBe(503);
    expect(await response.json()).toMatchObject({ status: 503, title: "Service Unavailable" });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
