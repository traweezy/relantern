import { describe, expect, it } from "vitest";
import { parseDigestSnapshot } from "./contract";

describe("digest contract", () => {
  it("preserves immutable delivery and attempt fields", () => {
    const timestamp = "2026-08-29T12:00:00Z";
    const result = parseDigestSnapshot({
      digests: [
        {
          attemptCount: 2,
          channel: "discord",
          emptyBehavior: "all_clear",
          executiveSummary: "No material changes met the threshold.",
          generatedAt: timestamp,
          id: "01990000-0000-7000-8000-000000000002",
          itemLimit: 10,
          items: [],
          localDate: "2026-08-29",
          minimumScore: 0.5,
          occurrenceId: "01990000-0000-7000-8000-000000000003",
          providerIdempotencyKey: "digest:fixture:discord",
          rendered: {
            channel: "discord",
            executiveSummary: "No material changes met the threshold.",
            generatedAt: timestamp,
            items: [],
            localDate: "2026-08-29",
            title: "Relantern morning brief",
            windowEnd: timestamp,
            windowStart: "2026-08-28T12:00:00Z",
          },
          state: "failed",
          windowEnd: timestamp,
          windowStart: "2026-08-28T12:00:00Z",
        },
      ],
      generatedAt: timestamp,
    });
    expect(result.digests[0]).toMatchObject({ attemptCount: 2, state: "failed" });
  });
});
