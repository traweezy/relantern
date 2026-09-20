import { describe, expect, it } from "vitest";
import { demoSnapshot } from "../demo/demo-snapshot";
import {
  IntelligenceContractError,
  parseAlertHistoryPage,
  parseLiveSnapshot,
  parseStoryDetail,
  parseTodaySnapshot,
} from "./contract";

describe("intelligence API contract validation", () => {
  it("accepts the complete typed fixture contracts", () => {
    expect(parseTodaySnapshot(demoSnapshot.today).stories).toHaveLength(4);
    expect(parseTodaySnapshot(demoSnapshot.today).alerts).toHaveLength(1);
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

    expect(() =>
      parseTodaySnapshot({
        ...demoSnapshot.today,
        alerts: [{ ...demoSnapshot.today.alerts[0], sourceUrl: "javascript:alert(1)" }],
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

  it("accepts owner alert history and validates delivery status at the boundary", () => {
    const alert = demoSnapshot.today.alerts[0];
    const history = {
      alerts: [
        {
          ...alert,
          correctionReason: null,
          correctedAt: null,
          deliveries: [
            {
              channel: "discord",
              state: "sent",
              attemptCount: 1,
              deliveredAt: "2026-09-20T12:00:00Z",
              nextAttemptAt: null,
            },
          ],
        },
      ],
      nextCursor: "YWxlcnRzXzE",
    } as const;

    expect(parseAlertHistoryPage(history).alerts[0]?.deliveries[0]?.state).toBe("sent");
    expect(parseAlertHistoryPage(history).alerts[0]?.correctionReason).toBeNull();
    expect(parseAlertHistoryPage(history).nextCursor).toBe("YWxlcnRzXzE");
    const corrected = {
      ...history,
      alerts: [
        {
          ...history.alerts[0],
          correctionReason: "withdrawn",
          correctedAt: "2026-09-20T14:00:00Z",
          deliveries: [{ ...history.alerts[0].deliveries[0], state: "suppressed" }],
        },
      ],
    } as const;
    expect(parseAlertHistoryPage(corrected).alerts[0]?.correctionReason).toBe("withdrawn");
    expect(parseAlertHistoryPage(corrected).alerts[0]?.deliveries[0]?.state).toBe("suppressed");
    expect(
      parseAlertHistoryPage({
        ...corrected,
        alerts: [{ ...corrected.alerts[0], correctionReason: "severity_downgraded" }],
      }).alerts[0]?.correctionReason,
    ).toBe("severity_downgraded");
    for (const reason of ["no_longer_published", "severity_unconfirmed"] as const) {
      expect(
        parseAlertHistoryPage({
          ...corrected,
          alerts: [{ ...corrected.alerts[0], correctionReason: reason }],
        }).alerts[0]?.correctionReason,
      ).toBe(reason);
    }
    expect(() =>
      parseAlertHistoryPage({
        ...corrected,
        alerts: [{ ...corrected.alerts[0], correctedAt: null }],
      }),
    ).toThrow("correction reason and time must appear together");
    expect(() =>
      parseAlertHistoryPage({
        ...corrected,
        alerts: [{ ...corrected.alerts[0], correctionReason: "invalid" }],
      }),
    ).toThrow(IntelligenceContractError);
    expect(() =>
      parseAlertHistoryPage({
        ...history,
        alerts: [{ ...history.alerts[0], sourceUrl: "javascript:alert(1)" }],
      }),
    ).toThrow("must use HTTPS outside local fixtures");
    expect(() =>
      parseAlertHistoryPage({
        ...history,
        alerts: [
          {
            ...history.alerts[0],
            deliveries: [{ ...history.alerts[0].deliveries[0], state: "unknown" }],
          },
        ],
      }),
    ).toThrow(IntelligenceContractError);
    expect(() => parseAlertHistoryPage({ ...history, nextCursor: "bad/cursor" })).toThrow(
      IntelligenceContractError,
    );
  });
});
