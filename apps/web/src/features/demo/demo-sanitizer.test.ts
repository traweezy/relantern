import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { demoSnapshot } from "./demo-snapshot";

const demoDirectory = fileURLToPath(new URL(".", import.meta.url));
const demoRouteDirectory = fileURLToPath(new URL("../../app/(public)/demo/", import.meta.url));
const forbiddenImportPatterns = [
  /@\/server\//,
  /@\/lib\/auth/,
  /better-auth/,
  /EventSource/,
  /from ["']node:/,
  /openai/i,
  /process\.env/,
  /\/api\/v1/,
  /\/api\/auth/,
] as const;

const productionModules = (directory: string): string[] => {
  const modules: string[] = [];
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      modules.push(...productionModules(path));
    } else if (
      (entry.name.endsWith(".ts") || entry.name.endsWith(".tsx")) &&
      !entry.name.endsWith(".test.ts")
    ) {
      modules.push(path);
    }
  }
  return modules;
};
const forbiddenFixturePatterns = [
  /5276132/,
  /railway\.internal/i,
  /@github\.relantern\.local/i,
  /provider_response/i,
  /secret/i,
] as const;

const collectTimestamps = (value: unknown, collected: string[] = []): string[] => {
  if (typeof value === "string" && /^\d{4}-\d{2}-\d{2}T/.test(value)) {
    collected.push(value);
  } else if (Array.isArray(value)) {
    for (const item of value) {
      collectTimestamps(item, collected);
    }
  } else if (typeof value === "object" && value !== null) {
    for (const item of Object.values(value)) {
      collectTimestamps(item, collected);
    }
  }
  return collected;
};

describe("isolated demonstration snapshot", () => {
  it("contains no configured owner, secret, provider, or private-host material", () => {
    const encoded = JSON.stringify(demoSnapshot);
    for (const pattern of forbiddenFixturePatterns) {
      expect(encoded).not.toMatch(pattern);
    }
  });

  it("uses deliberately non-current example timestamps", () => {
    const timestamps = collectTimestamps(demoSnapshot);
    expect(timestamps.length).toBeGreaterThan(0);
    for (const timestamp of timestamps) {
      expect(Date.parse(timestamp)).toBeLessThan(Date.parse("2026-01-01T00:00:00Z"));
    }
  });

  it("keeps demo production modules out of private and live dependency boundaries", () => {
    const modulePaths = [
      ...productionModules(demoDirectory),
      ...productionModules(demoRouteDirectory),
    ];
    for (const modulePath of modulePaths) {
      const source = readFileSync(modulePath, "utf8");
      for (const pattern of forbiddenImportPatterns) {
        expect(source, `${modulePath} matched ${pattern}`).not.toMatch(pattern);
      }
    }
  });
});
