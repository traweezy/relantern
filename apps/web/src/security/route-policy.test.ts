import { describe, expect, it } from "vitest";
import { isProtectedAPIRoute, isPublicRoute } from "./route-policy";

describe("public route policy", () => {
  it.each([
    "/demo",
    "/demo/opengraph-image",
    "/demo/story/go-toolchain-security",
    "/demo/story/unknown-fixture",
    "/healthz",
    "/login",
    "/api/auth/sign-in/social",
    "/api/auth/callback/github",
    "/api/webhooks/openai",
  ])("allows %s", (pathname) => {
    expect(isPublicRoute(pathname)).toBe(true);
  });

  it.each([
    "/",
    "/settings",
    "/demo/private",
    "/demo/story/UPPERCASE",
    "/demo/story/known%2Ffixture",
    "/demo/story/known\\fixture",
    "/demo//story/go-toolchain-security",
    "/demo/story/../login",
    "/healthz/private",
    "/api/private",
    "/api/webhooks/openai/replay",
  ])("protects %s", (pathname) => {
    expect(isPublicRoute(pathname)).toBe(false);
  });

  it("distinguishes protected API requests", () => {
    expect(isProtectedAPIRoute("/api/private")).toBe(true);
    expect(isProtectedAPIRoute("/api/auth/session")).toBe(false);
    expect(isProtectedAPIRoute("/settings")).toBe(false);
  });
});
