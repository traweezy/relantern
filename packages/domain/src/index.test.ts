import { describe, expect, it } from "vitest";
import { applyItemCommand, type ItemState } from "./index";

const initialState = {
  isRead: false,
  location: "inbox",
  starred: true,
} as const satisfies ItemState;

describe("applyItemCommand", () => {
  it("keeps stars independent when an item is archived", () => {
    expect(applyItemCommand(initialState, { type: "archive" })).toEqual({
      isRead: false,
      location: "archive",
      starred: true,
    });
  });

  it("changes reading state without changing location or importance", () => {
    expect(applyItemCommand(initialState, { type: "mark-read", value: true })).toEqual({
      isRead: true,
      location: "inbox",
      starred: true,
    });
  });
});
