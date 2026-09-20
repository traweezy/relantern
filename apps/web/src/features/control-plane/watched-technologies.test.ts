import type { WatchedTechnology } from "@relantern/domain";
import { describe, expect, it } from "vitest";
import { settingsCommandSchema } from "./commands";
import { parseTechnologies, technologiesToText } from "./watched-technologies";

const typedWatch: WatchedTechnology = {
  currentVersion: "1.27.0",
  ecosystem: "go",
  id: "01991234-5678-7abc-8def-0123456789ab",
  lastVerifiedAt: "2026-09-20T12:00:00Z",
  packageName: "golang.org/x/crypto",
  source: "owner-settings",
  status: "active",
  technology: "Go",
  versionConstraint: ">=1.27",
};

const legacyWatch: WatchedTechnology = {
  ...typedWatch,
  currentVersion: "18",
  ecosystem: null,
  id: "01991234-5678-7abc-8def-0123456789ac",
  packageName: "postgresql",
  technology: "PostgreSQL",
  versionConstraint: "",
};

describe("watched technology ecosystem", () => {
  it("round trips typed and legacy rows without inferring a legacy ecosystem", () => {
    const parsed = parseTechnologies(technologiesToText([typedWatch, legacyWatch]), [
      typedWatch,
      legacyWatch,
    ]);
    expect(parsed.map((watch) => watch.ecosystem)).toEqual(["go", null]);
    expect(parsed[0]?.lastVerifiedAt).toBe(typedWatch.lastVerifiedAt);
    expect(parsed[1]?.id).toBe(legacyWatch.id);
    expect(settingsCommandSchema.shape.technologies.safeParse(parsed).success).toBe(true);
  });

  it("rejects unsupported ecosystems and the former five-column format", () => {
    expect(() => parseTechnologies("Go | package | jvm | 1.0 | >=1 | active", [])).toThrow(
      /unsupported advisory ecosystem/,
    );
    expect(() => parseTechnologies("Go | package | 1.0 | >=1 | active", [])).toThrow(/six columns/);
    expect(
      settingsCommandSchema.shape.technologies.safeParse([{ ...typedWatch, ecosystem: "jvm" }])
        .success,
    ).toBe(false);
  });

  it("clears stale verification when the ecosystem changes", () => {
    const parsed = parseTechnologies("Go | golang.org/x/crypto | npm | 1.27.0 | >=1.27 | active", [
      typedWatch,
    ]);
    expect(parsed[0]?.lastVerifiedAt).toBeUndefined();
  });

  it("preserves the identity of the matching ecosystem when package names overlap", () => {
    const npmWatch: WatchedTechnology = {
      ...typedWatch,
      ecosystem: "npm",
      id: "01991234-5678-7abc-8def-0123456789ae",
      technology: "npm",
    };
    const parsed = parseTechnologies(technologiesToText([npmWatch]), [typedWatch, npmWatch]);
    expect(parsed[0]?.id).toBe(npmWatch.id);
    expect(parsed[0]?.lastVerifiedAt).toBe(npmWatch.lastVerifiedAt);
  });
});
