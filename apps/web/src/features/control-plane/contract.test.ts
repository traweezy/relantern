import { describe, expect, it } from "vitest";
import { parseOperationsSnapshot, parseSourcesSnapshot } from "./contract";

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
});
