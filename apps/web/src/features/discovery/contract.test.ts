import { describe, expect, it } from "vitest";
import { parseReleaseCatalog, parseSearchResponse } from "./contract";

describe("discovery contracts", () => {
  it("parses bounded search results", () => {
    expect(
      parseSearchResponse({
        explanation: "Hybrid retrieval.",
        query: "postgres replication",
        resultCount: 1,
        results: [
          {
            explanation: {
              keywordRank: 1,
              semanticRank: 2,
              semanticSimilarity: 0.88,
              summary: "Matched terms and meaning.",
            },
            firstSeenAt: "2026-08-29T12:00:00Z",
            itemId: "00000000-0000-4000-8000-000000000011",
            lifecycleState: "stable",
            packageName: "postgresql",
            recommendedAction: "Review the upgrade guide.",
            saved: false,
            score: 0.03,
            signal: "release",
            sourceTier: "T0",
            storyId: "00000000-0000-4000-8000-000000000012",
            summary: "PostgreSQL replication behavior changed.",
            title: "PostgreSQL replication update",
          },
        ],
      }).resultCount,
    ).toBe(1);
  });

  it("rejects model-predicted coming-soon dates without timestamps", () => {
    expect(() =>
      parseReleaseCatalog({
        comingSoon: [],
        generatedAt: "not-a-time",
        technologies: [],
      }),
    ).toThrow();
  });
});
