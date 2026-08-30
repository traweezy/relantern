import { describe, expect, it } from "vitest";
import { parseOperationsSnapshot, parseRadarSnapshot, parseSourcesSnapshot } from "./contract";

describe("control-plane contracts", () => {
  it("parses an empty source registry snapshot", () => {
    expect(
      parseSourcesSnapshot({ generatedAt: "2026-08-29T12:00:00Z", sources: [] }).sources,
    ).toHaveLength(0);
  });

  it("rejects operations with a malformed deployment identity", () => {
    expect(() =>
      parseOperationsSnapshot({
        deliveryAttempts: 0,
        deployment: { environment: "test", gitSha: "", version: "test" },
        generatedAt: "2026-08-29T12:00:00Z",
        occurrences: [],
        openaiBackgroundPending: 0,
        queues: [],
        restore: { explanation: "Not rehearsed.", state: "not_recorded" },
        schedules: [],
        sourceErrorBudgets: [],
        stuckJobs: [],
      }),
    ).toThrow();
  });

  it("parses an empty Radar ledger without inventing candidates", () => {
    expect(
      parseRadarSnapshot({
        candidates: [],
        generatedAt: "2026-08-29T12:00:00Z",
        reviewDue: 0,
        runs: [],
        states: ["adopt", "trial", "assess", "hold", "reject"],
      }).candidates,
    ).toHaveLength(0);
  });

  it("rejects a Radar comparison missing a required dimension", () => {
    expect(() =>
      parseRadarSnapshot({
        candidates: [
          {
            currentState: "assess",
            decisions: [],
            discoveredAt: "2026-08-29T12:00:00Z",
            discoverySource: "fixture",
            ecosystem: "npm",
            id: "01991234-5678-7abc-8def-0123456789bd",
            incumbentPackage: "incumbent",
            latestComparison: {
              assessedAt: "2026-08-29T12:00:00Z",
              confidence: 1,
              dimensions: [],
              evidence: [],
              id: "01991234-5678-7abc-8def-0123456789be",
              misleading: false,
              suggestedState: "assess",
            },
            packageName: "candidate",
            repositoryUrl: "https://example.test/candidate",
            reviewAt: "2026-09-29T12:00:00Z",
            version: 1,
          },
        ],
        generatedAt: "2026-08-29T12:00:00Z",
        reviewDue: 0,
        runs: [],
        states: ["adopt", "trial", "assess", "hold", "reject"],
      }),
    ).toThrow();
  });
});
