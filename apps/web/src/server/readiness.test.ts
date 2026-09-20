import { describe, expect, it, vi } from "vitest";

vi.mock("server-only", () => ({}));

import { createReadinessProbe } from "./readiness";

const releaseSHA = "a".repeat(40);
const readyResponse = (gitSha = releaseSHA): Response => Response.json({ gitSha, status: "ready" });

const setup = (environment: string = "staging", gitSHA: string = releaseSHA) => {
  const clock = { now: 0 };
  const request = vi.fn<typeof fetch>().mockImplementation(async () => readyResponse(gitSHA));
  const onFailure = vi.fn();
  const baseURL = vi.fn(() => "http://api:8080");
  const probe = createReadinessProbe({
    baseURL,
    environment: () => ({ APP_ENV: environment, GIT_SHA: gitSHA }),
    now: () => clock.now,
    onFailure,
    request,
  });
  return { baseURL, clock, onFailure, probe, request };
};

describe("private API readiness probe", () => {
  it.each(["staging", "production"])(
    "matches the private API release in %s",
    async (environment) => {
      const { probe, request } = setup(environment);

      expect(await probe()).toEqual({ ready: true });
      const [url, init] = request.mock.calls[0] ?? [];
      expect(url?.toString()).toBe("http://api:8080/readyz");
      expect(init).toMatchObject({
        cache: "no-store",
        headers: { accept: "application/json" },
        redirect: "error",
      });
      expect(init?.signal).toBeInstanceOf(AbortSignal);
    },
  );

  it("accepts a matching full 64-character release SHA", async () => {
    const { probe } = setup("staging", "c".repeat(64));

    expect(await probe()).toEqual({ ready: true });
  });

  it.each(["local", "test"])("probes the API in %s without a release SHA", async (environment) => {
    const { probe, request } = setup(environment, "unknown");
    request.mockResolvedValue(Response.json({ status: "ready" }));

    expect(await probe()).toEqual({ ready: true });
  });

  it("rejects an invalid hosted SHA before probing", async () => {
    const { onFailure, probe, request } = setup("staging", "unknown");

    expect(await probe()).toEqual({ ready: false, reason: "invalid-release-sha" });
    expect(onFailure).toHaveBeenCalledOnce();
    expect(request).not.toHaveBeenCalled();
  });

  it("rejects an unrecognized environment", async () => {
    const { probe, request } = setup("typo");

    expect(await probe()).toEqual({ ready: false, reason: "invalid-environment" });
    expect(request).not.toHaveBeenCalled();
  });

  it("fails closed when the private API configuration is missing", async () => {
    const { baseURL, probe, request } = setup();
    baseURL.mockImplementation(() => {
      throw new Error("private token");
    });

    expect(await probe()).toEqual({ ready: false, reason: "invalid-private-api-configuration" });
    expect(request).not.toHaveBeenCalled();
  });

  it("rejects an unready API", async () => {
    const { probe, request } = setup();
    request.mockResolvedValue(Response.json({ status: "unready" }, { status: 503 }));

    expect(await probe()).toEqual({ ready: false, reason: "private-api-unready" });
  });

  it("rejects an invalid private API response", async () => {
    const { probe, request } = setup();
    request.mockResolvedValue(Response.json({ status: "unknown" }));

    expect(await probe()).toEqual({ ready: false, reason: "invalid-private-api-response" });
  });

  it("rejects a mismatched private API release", async () => {
    const { probe, request } = setup();
    request.mockResolvedValue(readyResponse("b".repeat(40)));

    expect(await probe()).toEqual({ ready: false, reason: "release-sha-mismatch" });
  });

  it("fails closed when the private request times out", async () => {
    const { probe, request } = setup();
    request.mockRejectedValue(new Error("private URL"));

    expect(await probe()).toEqual({ ready: false, reason: "private-api-unavailable" });
  });

  it("bounds successful checks to one private request per two seconds", async () => {
    const { clock, probe, request } = setup();

    expect(await probe()).toEqual({ ready: true });
    clock.now = 1_999;
    expect(await probe()).toEqual({ ready: true });
    expect(request).toHaveBeenCalledTimes(1);
    clock.now = 2_000;
    expect(await probe()).toEqual({ ready: true });
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("bounds failed checks for one second without masking recovery", async () => {
    const { clock, onFailure, probe, request } = setup();
    request.mockResolvedValueOnce(readyResponse("b".repeat(40)));

    expect(await probe()).toEqual({ ready: false, reason: "release-sha-mismatch" });
    clock.now = 999;
    expect(await probe()).toEqual({ ready: false, reason: "release-sha-mismatch" });
    expect(request).toHaveBeenCalledTimes(1);
    expect(onFailure).toHaveBeenCalledTimes(1);
    clock.now = 1_000;
    expect(await probe()).toEqual({ ready: true });
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("shares one in-flight request across concurrent probes", async () => {
    const { probe, request } = setup();
    let resolveResponse: ((response: Response) => void) | undefined;
    request.mockImplementation(
      () =>
        new Promise<Response>((resolve) => {
          resolveResponse = resolve;
        }),
    );

    const first = probe();
    const second = probe();
    expect(request).toHaveBeenCalledTimes(1);
    resolveResponse?.(readyResponse());
    expect(await Promise.all([first, second])).toEqual([{ ready: true }, { ready: true }]);
  });
});
