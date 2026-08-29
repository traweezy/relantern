export type ReadingLocation = "inbox" | "later" | "archive";

export type ItemState = Readonly<{
  isRead: boolean;
  location: ReadingLocation;
  starred: boolean;
}>;

export type ItemCommand =
  | Readonly<{ type: "archive" }>
  | Readonly<{ type: "mark-read"; value: boolean }>
  | Readonly<{ type: "move"; location: Exclude<ReadingLocation, "archive"> }>
  | Readonly<{ type: "star"; value: boolean }>;

export const applyItemCommand = (state: ItemState, command: ItemCommand): ItemState => {
  switch (command.type) {
    case "archive":
      return { ...state, location: "archive" };
    case "mark-read":
      return { ...state, isRead: command.value };
    case "move":
      return { ...state, location: command.location };
    case "star":
      return { ...state, starred: command.value };
  }
};
