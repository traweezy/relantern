import { describe, expect, it, vi } from "vitest";

vi.mock("server-only", () => ({}));

import { IntelligenceConfigurationError, loadIntelligenceAPIConfiguration } from "./config";

const fixtureEnvironment = (): Record<string, string> => ({
  APP_ENV: "local",
  INTERNAL_API_URL: "http://api:8080",
  WEB_INTERNAL_SERVICE_TOKEN: "s".repeat(32),
});

describe("private intelligence API configuration", () => {
  it("accepts the local private service origin", () => {
    expect(loadIntelligenceAPIConfiguration(fixtureEnvironment())).toEqual({
      baseURL: "http://api:8080",
      serviceToken: "s".repeat(32),
    });
  });

  it("loads a mounted service-token file without exposing its path", () => {
    const environment = fixtureEnvironment();
    delete environment.WEB_INTERNAL_SERVICE_TOKEN;
    environment.WEB_INTERNAL_SERVICE_TOKEN_FILE = "/run/secrets/token";
    expect(
      loadIntelligenceAPIConfiguration(environment, () => `${"f".repeat(32)}\n`).serviceToken,
    ).toBe("f".repeat(32));
  });

  it("rejects weak credentials, ambiguous sources, and unsafe origins", () => {
    expect(() =>
      loadIntelligenceAPIConfiguration({
        ...fixtureEnvironment(),
        WEB_INTERNAL_SERVICE_TOKEN: "short",
      }),
    ).toThrow(IntelligenceConfigurationError);
    expect(() =>
      loadIntelligenceAPIConfiguration({
        ...fixtureEnvironment(),
        WEB_INTERNAL_SERVICE_TOKEN_FILE: "/run/secrets/token",
      }),
    ).toThrow("mutually exclusive");
    expect(() =>
      loadIntelligenceAPIConfiguration({
        ...fixtureEnvironment(),
        APP_ENV: "production",
        INTERNAL_API_URL: "http://public.example.com",
      }),
    ).toThrow("must use HTTPS");
  });
});
