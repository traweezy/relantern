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

export type SearchExplanation = Readonly<{
  keywordRank: number | null;
  semanticRank: number | null;
  semanticSimilarity: number | null;
  summary: string;
}>;

export type IntelligenceSearchResult = Readonly<{
  explanation: SearchExplanation;
  firstSeenAt: string;
  itemId: string;
  lifecycleState: "deprecated" | "eol" | "preview" | "release_candidate" | "stable" | "unknown";
  packageName: string;
  recommendedAction: string;
  saved: boolean;
  score: number;
  signal: StorySignal;
  sourceTier: SourceTier;
  storyId: string;
  summary: string;
  title: string;
}>;

export type IntelligenceSearchResponse = Readonly<{
  explanation: string;
  query: string;
  resultCount: number;
  results: readonly IntelligenceSearchResult[];
}>;

export type IntelligenceSearchFilters = Readonly<{
  action?: string | undefined;
  after?: string | undefined;
  before?: string | undefined;
  lifecycle?: string | undefined;
  radar?: string | undefined;
  saved?: string | undefined;
  sourceTier?: string | undefined;
  topic?: string | undefined;
}>;

export type SavedIntelligenceSearch = Readonly<{
  createdAt: string;
  filters: IntelligenceSearchFilters;
  id: string;
  name: string;
  query: string;
  updatedAt: string;
}>;

export type ReleaseState =
  | "deprecation"
  | "deprecated"
  | "eol"
  | "preview"
  | "release_candidate"
  | "security"
  | "stable"
  | "unknown";

export type ReleaseEntry = Readonly<{
  headline: string;
  lastVerifiedAt: string;
  packageName: string;
  sourceTier: SourceTier;
  sourceUrl: string;
  state: ReleaseState;
  storyId: string;
  summary: string;
  targetDate: string | null;
  technology: string;
  version: string;
}>;

export type TechnologyRelease = Readonly<{
  currentVersion: string;
  entries: readonly ReleaseEntry[];
  newestVersion: string;
  packageName: string;
  technology: string;
  upgradeStatus: "current" | "not-configured" | "update-available";
}>;

export type ReleaseCatalog = Readonly<{
  comingSoon: readonly ReleaseEntry[];
  generatedAt: string;
  technologies: readonly TechnologyRelease[];
}>;

export type ManualCapture = Readonly<{
  completedAt?: string | undefined;
  createdAt: string;
  errorCode?: string | undefined;
  id: string;
  state: "completed" | "deduplicating" | "failed" | "fetching" | "parsing" | "queued";
  storyId?: string | undefined;
  url: string;
}>;

export type ImportCandidate = Readonly<{
  connector: string;
  duplicate: boolean;
  explanation: string;
  id: string;
  name: string;
  url: string;
  valid: boolean;
}>;

export type ImportPreview = Readonly<{
  candidates: readonly ImportCandidate[];
  expiresAt: string;
  id: string;
}>;

export type ImportCommitResult = Readonly<{
  importedCount: number;
  pendingSourceIds: readonly string[];
}>;

export type SourcePreference = Readonly<{
  excludeFromDigest: boolean;
  muted: boolean;
  relevanceAdjustment: number;
  version: number;
}>;

export type SourceEndpointHealth = Readonly<{
  connector: string;
  healthState: string;
  id: string;
  lastAttemptAt?: string | undefined;
  lastSuccessAt?: string | undefined;
  latestErrorCode?: string | undefined;
  latestStatusCode?: number | undefined;
  nextPollAt?: string | undefined;
  pollIntervalSeconds: number;
  priority: string;
  url: string;
}>;

export type SourceValidation = Readonly<{
  checkCount: number;
  completedAt: string;
  explanation: string;
  failedCheckCount: number;
  id: string;
  state: "failed" | "passed";
}>;

export type ManagedSource = Readonly<{
  contentCount: number;
  contentPolicy: string;
  duplicateRate: number;
  enabled: boolean;
  endpoints: readonly SourceEndpointHealth[];
  errorBudgetState: "exhausted" | "healthy" | "unknown" | "warning";
  failureCount24h: number;
  homepageUrl: string;
  id: string;
  latestValidation?: SourceValidation | undefined;
  name: string;
  origin: "owner" | "system";
  owner: string;
  pollingEnabled: boolean;
  preference: SourcePreference;
  reviewedAt?: string | undefined;
  successCount24h: number;
  topics: readonly string[];
  trustTier: SourceTier;
  validationState: "active" | "degraded" | "paused" | "pending" | "rejected";
}>;

export type SourcesSnapshot = Readonly<{
  generatedAt: string;
  sources: readonly ManagedSource[];
}>;

export type InterestTopic = Readonly<{
  exclusions: readonly string[];
  keywords: readonly string[];
  priority: number;
  topicId: string;
  weight: number;
}>;

export type InterestProfile = Readonly<{
  id: string;
  name: string;
  profileSummary: string;
  topics: readonly InterestTopic[];
  version: number;
}>;

export type WatchedTechnology = Readonly<{
  currentVersion: string;
  id: string;
  lastVerifiedAt?: string | undefined;
  packageName: string;
  source: string;
  status: "active" | "evaluating" | "legacy" | "planned";
  technology: string;
  versionConstraint: string;
}>;

export type OwnerSettings = Readonly<{
  auditRetentionDays: number;
  criticalAlertsBypass: boolean;
  monthlyHardBudgetUsd: string;
  monthlySoftBudgetUsd: string;
  quietHoursEnd: string;
  quietHoursStart: string;
  rawRetentionDays: number;
  timezone: string;
  version: number;
}>;

export type ScheduleDefinition = Readonly<{
  catchupGraceMinutes: number;
  catchupPolicy: "catch_up" | "skip";
  channels: readonly ("dashboard" | "discord" | "email")[];
  daysOfWeek: readonly number[];
  emptyBehavior: "all_clear" | "dashboard_only" | "send_nothing";
  enabled: boolean;
  id: string;
  includeComingSoon: boolean;
  includeLaterReminders: boolean;
  includeRadarCandidates: boolean;
  localTime: string;
  maximumItems: 5 | 10 | 15 | 20;
  minimumScore: number;
  nextDueAt: string;
  pausedAt?: string | undefined;
  pausedUntil?: string | undefined;
  scheduleType: "daily_digest" | "maintenance" | "weekly_radar";
  skipNextAt?: string | undefined;
  timezone: string;
  version: number;
  weekendMode: "normal" | "off" | "weekly_only";
}>;

export type SettingsSnapshot = Readonly<{
  generatedAt: string;
  owner: OwnerSettings;
  profile: InterestProfile;
  schedules: readonly ScheduleDefinition[];
  technologies: readonly WatchedTechnology[];
}>;

export type SchedulePreview = Readonly<{
  candidateCount: number;
  explanation: string;
  externalDelivery: boolean;
  localDate: string;
  maximumItems: number;
  nextRunAt: string;
  scheduleId: string;
  scheduleType: string;
}>;

export type ScheduleActionResult = Readonly<{
  message: string;
  occurrenceId?: string | undefined;
  schedule: ScheduleDefinition;
}>;

export type QueueDepth = Readonly<{
  available: number;
  cancelled: number;
  completed: number;
  discarded: number;
  queue: string;
  retryable: number;
  running: number;
  scheduled: number;
}>;

export type StuckJob = Readonly<{
  attempt: number;
  attemptedAt?: string | undefined;
  id: number;
  kind: string;
  queue: string;
}>;

export type SourceErrorBudget = Readonly<{
  attempts: number;
  failureRate: number;
  failures: number;
  sourceId: string;
  sourceName: string;
  state: "exhausted" | "healthy" | "unknown" | "warning";
}>;

export type DeploymentMetadata = Readonly<{
  environment: string;
  gitSha: string;
  version: string;
}>;

export type ScheduleOccurrence = Readonly<{
  completedAt?: string | undefined;
  errorCode?: string | undefined;
  id: string;
  idempotencyKey: string;
  localDate: string;
  scheduleId: string;
  scheduleType: string;
  scheduledFor: string;
  startedAt?: string | undefined;
  state: string;
  triggerType: string;
}>;

export type OperationsSnapshot = Readonly<{
  deliveryAttempts: number;
  deployment: DeploymentMetadata;
  generatedAt: string;
  occurrences: readonly ScheduleOccurrence[];
  oldestOverdueOccurrence?: string | undefined;
  openaiBackgroundPending: number;
  queues: readonly QueueDepth[];
  restore: Readonly<{ explanation: string; state: string }>;
  schedulerLastSuccess?: string | undefined;
  schedules: readonly ScheduleDefinition[];
  sourceErrorBudgets: readonly SourceErrorBudget[];
  stuckJobs: readonly StuckJob[];
}>;

export type RadarState = "adopt" | "assess" | "hold" | "reject" | "trial";

export type RadarEvidenceLink = Readonly<{
  label: string;
  sourceTier: "T0" | "T1" | "T2" | "T3";
  url: string;
}>;

export type RadarComparisonDimension = Readonly<{
  candidate: string;
  current: string;
  evidence: readonly RadarEvidenceLink[];
  name:
    | "Capability"
    | "Maintenance"
    | "Migration"
    | "Performance"
    | "Reversibility"
    | "Security"
    | "Stability";
  verdict: "needs-evidence" | "supported";
}>;

export type RadarMetricSnapshot = Readonly<{
  bundleSizeBytes?: number | undefined;
  contributorCount: number;
  id: string;
  license: string;
  maintenance: Readonly<Record<string, unknown>>;
  observedAt: string;
  popularity: Readonly<Record<string, unknown>>;
  provenance: Readonly<Record<string, unknown>>;
  releaseVersion: string;
  runtimeCompatibility: readonly string[];
  security: Readonly<Record<string, unknown>>;
  typesSupported: boolean;
}>;

export type RadarComparison = Readonly<{
  assessedAt: string;
  confidence: number;
  dimensions: readonly RadarComparisonDimension[];
  evidence: readonly RadarEvidenceLink[];
  id: string;
  misleading: boolean;
  suggestedState: "assess" | "hold" | "reject";
}>;

export type RadarDecision = Readonly<{
  applicableProjectTypes: readonly string[];
  compatibilityRequirements: readonly string[];
  decidedAt: string;
  decisionSource: "owner" | "system";
  evidence: Readonly<Record<string, unknown>>;
  exitConditions: readonly string[];
  id: string;
  rationale: string;
  reviewAt: string;
  state: RadarState;
}>;

export type RadarCandidate = Readonly<{
  currentState: RadarState;
  decisions: readonly RadarDecision[];
  discoveredAt: string;
  discoverySource: string;
  ecosystem: "go" | "jvm" | "npm" | "other" | "python" | "rust";
  id: string;
  incumbentPackage: string;
  latestComparison?: RadarComparison | undefined;
  latestMetric?: RadarMetricSnapshot | undefined;
  packageName: string;
  repositoryUrl: string;
  reviewAt: string;
  version: number;
}>;

export type RadarDiscoveryRun = Readonly<{
  candidateCount: number;
  completedAt?: string | undefined;
  errorCode?: string | undefined;
  evidenceCount: number;
  id: string;
  misleadingCount: number;
  requestedAt: string;
  startedAt?: string | undefined;
  state: "completed" | "failed" | "queued" | "running";
  triggerType: "owner" | "scheduled";
}>;

export type RadarSnapshot = Readonly<{
  candidates: readonly RadarCandidate[];
  generatedAt: string;
  reviewDue: number;
  runs: readonly RadarDiscoveryRun[];
  states: readonly RadarState[];
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
