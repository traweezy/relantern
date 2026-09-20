import { afterEach, describe, expect, it, vi } from "vitest";

const readinessProbe = vi.hoisted(() => vi.fn());

vi.mock("server-only", () => ({}));
vi.mock("next/server", () => ({ connection: vi.fn().mockResolvedValue(undefined) }));
vi.mock("@/server/intelligence/config", () => ({ getIntelligenceAPIConfiguration: vi.fn() }));
vi.mock("@/server/readiness", () => ({ createReadinessProbe: () => readinessProbe }));

import { GET } from "./route";

afterEach(() => {
  vi.clearAllMocks();
});

describe("web readiness route", () => {
  it("returns a no-store success without exposing the private API SHA", async () => {
    readinessProbe.mockResolvedValue({ ready: true });

    const response = await GET();

    expect(response.status).toBe(200);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    expect(await response.json()).toEqual({ service: "web", status: "ready" });
  });

  it("returns a sanitized uncached problem response", async () => {
    readinessProbe.mockResolvedValue({ ready: false, reason: "release-sha-mismatch" });

    const response = await GET();

    expect(response.status).toBe(503);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    expect(response.headers.get("Content-Type")).toBe("application/problem+json");
    expect(await response.json()).toEqual({
      detail: "A required private dependency is unavailable.",
      instance: "/readyz",
      status: 503,
      title: "Service Unavailable",
      type: "about:blank",
    });
  });
});
