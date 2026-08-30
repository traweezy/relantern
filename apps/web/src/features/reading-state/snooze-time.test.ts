import { describe, expect, test } from "vitest";
import { customSnoozeTime, snoozePresetTime } from "./snooze-time";

describe("owner-timezone snooze calculations", () => {
  test("keeps tomorrow morning at 08:00 across spring DST", () => {
    expect(snoozePresetTime("tomorrow", "America/New_York", new Date("2026-03-07T15:00:00Z"))).toBe(
      "2026-03-08T12:00:00.000Z",
    );
  });

  test("keeps next week at 08:00 across fall DST", () => {
    expect(snoozePresetTime("week", "America/New_York", new Date("2026-10-31T14:00:00Z"))).toBe(
      "2026-11-07T13:00:00.000Z",
    );
  });

  test("interprets custom values in the owner timezone", () => {
    expect(customSnoozeTime("2026-08-30T08:15", "America/New_York")).toBe(
      "2026-08-30T12:15:00.000Z",
    );
  });

  test("rejects a local time skipped by DST", () => {
    expect(() => customSnoozeTime("2026-03-08T02:30", "America/New_York")).toThrow(
      "does not exist",
    );
  });
});
