import { describe, expect, it, vi } from "vitest";

vi.mock("server-only", () => ({}));

import { loadPublicBaseURL, PublicConfigurationError } from "./public-config";

describe("public origin configuration", () => {
  it("uses a loopback origin only for local defaults", () => {
    expect(loadPublicBaseURL({ APP_ENV: "local" }).origin).toBe("http://127.0.0.1:3000");
  });

  it("accepts a hosted HTTPS origin", () => {
    expect(
      loadPublicBaseURL({ APP_ENV: "production", PUBLIC_BASE_URL: "https://relantern.example" })
        .origin,
    ).toBe("https://relantern.example");
  });

  it("fails closed for missing or unsafe hosted origins", () => {
    expect(() => loadPublicBaseURL({ APP_ENV: "production" })).toThrow(PublicConfigurationError);
    expect(() =>
      loadPublicBaseURL({
        APP_ENV: "production",
        PUBLIC_BASE_URL: "http://relantern.example",
      }),
    ).toThrow("must use HTTPS");
  });
});
