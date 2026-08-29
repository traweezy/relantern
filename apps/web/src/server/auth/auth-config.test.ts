import { describe, expect, it, vi } from "vitest";

vi.mock("server-only", () => ({}));

import { AuthConfigurationError, loadAuthConfiguration } from "./auth-config";

const fixtureEnvironment = (): Record<string, string> => ({
  APP_ENV: "local",
  AUTH_ALLOWED_GITHUB_USER_ID: "5276132",
  BETTER_AUTH_SECRET: "b".repeat(32),
  BETTER_AUTH_URL: "http://127.0.0.1:3000",
  DATABASE_URL: "postgresql://relantern:password@127.0.0.1:5432/relantern?sslmode=disable",
  LOCAL_OAUTH_STUB_SECRET: "o".repeat(32),
});

describe("auth configuration", () => {
  it("loads the safe local fixture defaults", () => {
    const configuration = loadAuthConfiguration(fixtureEnvironment());
    expect(configuration).toMatchObject({
      allowedGitHubUserID: "5276132",
      baseURL: "http://127.0.0.1:3000",
      oauthClientID: "relantern-local",
      providerMode: "fixture",
      secureCookies: false,
      sessionMaxAgeSeconds: 604800,
    });
    expect(configuration.oauthTokenURL).toBe("http://127.0.0.1:8090/oauth/token");
  });

  it("fails closed for malformed owner IDs", () => {
    expect(() =>
      loadAuthConfiguration({
        ...fixtureEnvironment(),
        AUTH_ALLOWED_GITHUB_USER_ID: "owner-name",
      }),
    ).toThrow(AuthConfigurationError);
  });

  it("requires an explicit live-provider fuse in local development", () => {
    expect(() =>
      loadAuthConfiguration({
        ...fixtureEnvironment(),
        AUTH_PROVIDER_MODE: "github",
        GITHUB_OAUTH_CLIENT_ID: "client",
        GITHUB_OAUTH_CLIENT_SECRET: "g".repeat(32),
      }),
    ).toThrow("ALLOW_LIVE_EXTERNAL_APIS=true");
  });

  it("requires HTTPS and real GitHub credentials in production", () => {
    expect(() =>
      loadAuthConfiguration({
        ...fixtureEnvironment(),
        APP_ENV: "production",
        AUTH_PROVIDER_MODE: "github",
      }),
    ).toThrow("BETTER_AUTH_URL must use HTTPS");

    const configuration = loadAuthConfiguration({
      ...fixtureEnvironment(),
      APP_ENV: "production",
      AUTH_PROVIDER_MODE: "github",
      BETTER_AUTH_URL: "https://app.relantern.example",
      GITHUB_OAUTH_CLIENT_ID: "production-client",
      GITHUB_OAUTH_CLIENT_SECRET: "g".repeat(32),
    });
    expect(configuration.providerMode).toBe("github");
    expect(configuration.secureCookies).toBe(true);
  });

  it("does not expose unreadable secret paths", () => {
    expect(() =>
      loadAuthConfiguration(
        {
          ...fixtureEnvironment(),
          BETTER_AUTH_SECRET: "",
          BETTER_AUTH_SECRET_FILE: "/private/secret",
        },
        () => {
          throw new Error("contains-sensitive-detail");
        },
      ),
    ).toThrow("BETTER_AUTH_SECRET_FILE could not be read");
  });
});
