export type ReadingLocation = "inbox" | "later" | "archive";

export type ReadingAction =
  | "add_tag"
  | "already_known"
  | "archive"
  | "dismiss"
  | "mark_read"
  | "mark_unread"
  | "move_inbox"
  | "move_later"
  | "remove_tag"
  | "snooze"
  | "star"
  | "unstar"
  | "unsnooze"
  | "update_progress";

export type StoryReadingState = Readonly<{
  dismissedReason: string | null;
  isRead: boolean;
  lastParagraphId: string | null;
  laterPosition: number | null;
  location: ReadingLocation;
  readAt: string | null;
  readingProgress: number;
  snoozedFromLocation: Exclude<ReadingLocation, "archive"> | null;
  snoozedUntil: string | null;
  starredAt: string | null;
  storyId: string;
  tagIds: readonly string[];
  updatedAt: string;
  version: number;
}>;

export type Confidence = "high" | "medium" | "low" | "unknown";

export type SourceTier = "T0" | "T1" | "T2" | "T3";

export type StorySignal = "breaking-change" | "deprecation" | "general" | "release" | "security";

export type StoryStatus = "new" | "updated";

export type StorySource = Readonly<{
  domain: string;
  label: string;
  tier: SourceTier;
  url: string;
}>;

export type ClaimEvidence = Readonly<{
  claim: string;
  material: boolean;
  sources: readonly StorySource[];
}>;

export type StorySummary = Readonly<{
  confidence: Confidence;
  firstSeenAt: string;
  headline: string;
  id: string;
  lastChangedAt: string;
  primarySourceUrl: string;
  readTimeMinutes: number;
  recommendedAction: string;
  signal: StorySignal;
  sourceCount: number;
  sourceTier: SourceTier;
  status: StoryStatus;
  summary: string;
  whyItMatters: string;
}>;

export type StoryDetail = StorySummary &
  Readonly<{
    assertions: readonly ClaimEvidence[];
    normalizedContent: string;
    related: readonly StorySummary[];
    revisionId: string;
    sources: readonly StorySource[];
    uncertainties: readonly string[];
  }>;

export type TodayStats = Readonly<{
  criticalAlerts: number;
  estimatedCostUsd: string;
  releases: number;
  reviewRequired: number;
  sourceCoverage: number;
}>;

export type TodaySnapshot = Readonly<{
  coverageEndAt: string;
  coverageStartAt: string;
  deliveryState: "delivered" | "pending" | "unavailable";
  generatedAt: string;
  nextRunAt: string | null;
  stats: TodayStats;
  stories: readonly StorySummary[];
}>;

export type LiveEvent = Readonly<{
  id: string;
  observedAt: string;
  story: StorySummary;
  type: "story-created" | "story-updated";
}>;

export type LiveSnapshot = Readonly<{
  events: readonly LiveEvent[];
  generatedAt: string;
}>;

export type StoryListItem = Readonly<{
  state: StoryReadingState;
  story: StorySummary;
}>;

export type ReadingCollection = Readonly<{
  items: readonly StoryListItem[];
  nextCursor: string | null;
  total: number;
}>;

export type ReadingMutation = Readonly<{
  mutationId: string;
  state: StoryReadingState;
  undoDeadline: string;
}>;

export type BulkReadingMutation = Readonly<{
  affectedCount: number;
  bulkId: string;
  mutations: readonly ReadingMutation[];
  undoDeadline: string;
}>;

export type StoryTag = Readonly<{
  colorToken: "accent" | "blue" | "green" | "orange" | "purple" | "red" | "slate";
  createdAt: string;
  id: string;
  name: string;
  updatedAt: string;
}>;

export type StoryFeedbackType =
  | "useful"
  | "already_known"
  | "irrelevant"
  | "too_shallow"
  | "too_verbose"
  | "incorrect";

export type StoryFeedback = Readonly<{
  createdAt: string;
  id: string;
  note: string | null;
  storyId: string;
  type: StoryFeedbackType;
}>;

export type AnnotationType = "document_note" | "highlight" | "highlight_note";

export type StoryAnnotation = Readonly<{
  body: string;
  createdAt: string;
  endOffset: number | null;
  id: string;
  orphaned: boolean;
  quoteHash: string | null;
  revisionId: string;
  startOffset: number | null;
  storyId: string;
  type: AnnotationType;
  updatedAt: string;
}>;

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
