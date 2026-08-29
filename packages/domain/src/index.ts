export type ReadingLocation = "inbox" | "later" | "archive";

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
    related: readonly StorySummary[];
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
