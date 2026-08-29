import { describe, expect, it } from "vitest";
import { isAllowedOwnerProfile, isAllowedOwnerSource, mapOwnerProfile } from "./owner-policy";

describe("owner profile policy", () => {
  it("accepts the configured numeric GitHub identity", () => {
    expect(isAllowedOwnerProfile({ id: 5_276_132, login: "traweezy" }, "5276132")).toBe(true);
    expect(isAllowedOwnerProfile({ id: "5276132", login: "traweezy" }, "5276132")).toBe(true);
  });

  it("rejects other or malformed identities", () => {
    expect(isAllowedOwnerProfile({ id: "5276133" }, "5276132")).toBe(false);
    expect(isAllowedOwnerProfile({ id: "0005276132" }, "5276132")).toBe(false);
    expect(isAllowedOwnerProfile({ id: Number.MAX_SAFE_INTEGER + 1 }, "5276132")).toBe(false);
    expect(isAllowedOwnerProfile(null, "5276132")).toBe(false);
  });

  it("admits only fresh GitHub OAuth sources", () => {
    expect(
      isAllowedOwnerSource(
        { method: "oauth", oauth: { profile: { id: "5276132" }, providerId: "github" } },
        "5276132",
      ),
    ).toBe(true);
    expect(
      isAllowedOwnerSource(
        { method: "email-password", oauth: { profile: { id: "5276132" }, providerId: "github" } },
        "5276132",
      ),
    ).toBe(false);
    expect(
      isAllowedOwnerSource(
        { method: "oauth", oauth: { profile: { id: "5276132" }, providerId: "gitlab" } },
        "5276132",
      ),
    ).toBe(false);
  });

  it("maps a provider profile without retaining the provider email", () => {
    expect(
      mapOwnerProfile(
        {
          avatar_url: "https://avatars.githubusercontent.com/u/5276132",
          email: "private@example.com",
          id: 5_276_132,
          login: "traweezy",
          name: "Relantern owner",
        },
        "America/New_York",
      ),
    ).toEqual({
      email: "5276132@github.relantern.local",
      emailVerified: true,
      githubUserId: 5_276_132,
      image: "https://avatars.githubusercontent.com/u/5276132",
      login: "traweezy",
      name: "Relantern owner",
      timezone: "America/New_York",
    });
  });

  it("rejects invalid logins and non-HTTPS avatar URLs", () => {
    expect(() => mapOwnerProfile({ id: "5276132", login: "invalid login" }, "UTC")).toThrow();
    expect(
      mapOwnerProfile({ avatar_url: "http://example.com/a.png", id: "5276132", login: "t" }, "UTC"),
    ).not.toHaveProperty("image");
  });
});
