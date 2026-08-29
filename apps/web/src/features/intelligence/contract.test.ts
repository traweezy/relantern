import { describe, expect, it } from "vitest";
import { demoSnapshot } from "../demo/demo-snapshot";
import {
  IntelligenceContractError,
  parseLiveSnapshot,
  parseStoryDetail,
  parseTodaySnapshot,
} from "./contract";

describe("intelligence API contract validation", () => {
  it("accepts the complete typed fixture contracts", () => {
    expect(parseTodaySnapshot(demoSnapshot.today).stories).toHaveLength(4);
    expect(parseLiveSnapshot(demoSnapshot.live).events).toHaveLength(4);
    expect(parseStoryDetail(demoSnapshot.stories["go-toolchain-security"]).assertions).toHaveLength(
      1,
    );
  });

  it("rejects malformed timestamps and non-HTTP evidence URLs", () => {
    expect(() =>
      parseTodaySnapshot({ ...demoSnapshot.today, generatedAt: "not-a-timestamp" }),
    ).toThrow(IntelligenceContractError);

    const story = demoSnapshot.stories["go-toolchain-security"];
    expect(() =>
      parseStoryDetail({
        ...story,
        sources: [{ ...story.sources[0], url: "javascript:alert(1)" }],
      }),
    ).toThrow("must use HTTPS outside local fixtures");
  });

  it("rejects unsupported signal values and excessive numeric fields", () => {
    expect(() =>
      parseTodaySnapshot({
        ...demoSnapshot.today,
        stats: { ...demoSnapshot.today.stats, sourceCoverage: 101 },
      }),
    ).toThrow(IntelligenceContractError);
    expect(() =>
      parseLiveSnapshot({
        ...demoSnapshot.live,
        events: [
          {
            ...demoSnapshot.live.events[0],
            story: { ...demoSnapshot.live.events[0]?.story, signal: "rumor" },
          },
        ],
      }),
    ).toThrow(IntelligenceContractError);
  });
});
