import type {
  ManagedSource,
  OperationsSnapshot,
  RadarCandidate,
  RadarDiscoveryRun,
  RadarSnapshot,
  ScheduleActionResult,
  ScheduleDefinition,
  SchedulePreview,
  SettingsSnapshot,
  SourcePreference,
  SourcesSnapshot,
} from "@relantern/domain";
import { z } from "zod";

const sourceTierSchema = z.enum(["T0", "T1", "T2", "T3"]);
const optionalTimeSchema = z.iso.datetime({ offset: true }).optional();
const errorBudgetStateSchema = z.enum(["exhausted", "healthy", "unknown", "warning"]);

const sourcePreferenceSchema: z.ZodType<SourcePreference> = z.object({
  excludeFromDigest: z.boolean(),
  muted: z.boolean(),
  relevanceAdjustment: z.number().min(-1).max(1),
  version: z.number().int().nonnegative(),
});

const sourceEndpointSchema = z.object({
  connector: z.string().min(1),
  healthState: z.string().min(1),
  id: z.uuid(),
  lastAttemptAt: optionalTimeSchema,
  lastSuccessAt: optionalTimeSchema,
  latestErrorCode: z.string().optional(),
  latestStatusCode: z.number().int().min(100).max(599).optional(),
  nextPollAt: optionalTimeSchema,
  pollIntervalSeconds: z.number().int().positive(),
  priority: z.string().min(1),
  url: z.url(),
});

const sourceValidationSchema = z.object({
  checkCount: z.number().int().positive(),
  completedAt: z.iso.datetime({ offset: true }),
  explanation: z.string().min(1),
  failedCheckCount: z.number().int().nonnegative(),
  id: z.uuid(),
  state: z.enum(["failed", "passed"]),
});

const managedSourceSchema: z.ZodType<ManagedSource> = z.object({
  contentCount: z.number().int().nonnegative(),
  contentPolicy: z.string().min(1),
  duplicateRate: z.number().min(0).max(1),
  enabled: z.boolean(),
  endpoints: z.array(sourceEndpointSchema),
  errorBudgetState: errorBudgetStateSchema,
  failureCount24h: z.number().int().nonnegative(),
  homepageUrl: z.url(),
  id: z.string().min(1),
  latestValidation: sourceValidationSchema.optional(),
  name: z.string().min(1),
  origin: z.enum(["owner", "system"]),
  owner: z.string().min(1),
  pollingEnabled: z.boolean(),
  preference: sourcePreferenceSchema,
  reviewedAt: optionalTimeSchema,
  successCount24h: z.number().int().nonnegative(),
  topics: z.array(z.string().min(1)),
  trustTier: sourceTierSchema,
  validationState: z.enum(["active", "degraded", "paused", "pending", "rejected"]),
});

const sourcesSnapshotSchema: z.ZodType<SourcesSnapshot> = z.object({
  generatedAt: z.iso.datetime({ offset: true }),
  sources: z.array(managedSourceSchema),
});

const interestTopicSchema = z.object({
  exclusions: z.array(z.string().min(1)),
  keywords: z.array(z.string().min(1)),
  priority: z.number().int().min(1).max(5),
  topicId: z.string().regex(/^[a-z0-9]+(?:[-_][a-z0-9]+)*$/),
  weight: z.number().min(0).max(1),
});

const watchedTechnologySchema = z.object({
  currentVersion: z.string().max(100),
  id: z.uuid(),
  lastVerifiedAt: optionalTimeSchema,
  packageName: z.string().min(1).max(255),
  source: z.string().min(1).max(120),
  status: z.enum(["active", "evaluating", "legacy", "planned"]),
  technology: z.string().min(1).max(120),
  versionConstraint: z.string().max(200),
});

export const scheduleDefinitionSchema: z.ZodType<ScheduleDefinition> = z.object({
  catchupGraceMinutes: z.number().int().min(0).max(10_080),
  catchupPolicy: z.enum(["catch_up", "skip"]),
  channels: z
    .array(z.enum(["dashboard", "discord", "email"]))
    .min(1)
    .max(3),
  daysOfWeek: z.array(z.number().int().min(1).max(7)).min(1).max(7),
  emptyBehavior: z.enum(["all_clear", "dashboard_only", "send_nothing"]),
  enabled: z.boolean(),
  id: z.uuid(),
  includeComingSoon: z.boolean(),
  includeLaterReminders: z.boolean(),
  includeRadarCandidates: z.boolean(),
  localTime: z.string().regex(/^\d{2}:\d{2}$/),
  maximumItems: z.union([z.literal(5), z.literal(10), z.literal(15), z.literal(20)]),
  minimumScore: z.number().min(0).max(1),
  nextDueAt: z.iso.datetime({ offset: true }),
  pausedAt: optionalTimeSchema,
  pausedUntil: optionalTimeSchema,
  scheduleType: z.enum(["daily_digest", "maintenance", "weekly_radar"]),
  skipNextAt: optionalTimeSchema,
  timezone: z.string().min(1),
  version: z.number().int().positive(),
  weekendMode: z.enum(["normal", "off", "weekly_only"]),
});

const settingsSnapshotSchema: z.ZodType<SettingsSnapshot> = z.object({
  generatedAt: z.iso.datetime({ offset: true }),
  owner: z.object({
    auditRetentionDays: z.number().int().min(30).max(3650),
    criticalAlertsBypass: z.boolean(),
    monthlyHardBudgetUsd: z.string().min(1),
    monthlySoftBudgetUsd: z.string().min(1),
    quietHoursEnd: z.string().regex(/^\d{2}:\d{2}$/),
    quietHoursStart: z.string().regex(/^\d{2}:\d{2}$/),
    rawRetentionDays: z.number().int().min(7).max(3650),
    timezone: z.string().min(1),
    version: z.number().int().positive(),
  }),
  profile: z.object({
    id: z.uuid(),
    name: z.string().min(1),
    profileSummary: z.string(),
    topics: z.array(interestTopicSchema).min(1),
    version: z.number().int().positive(),
  }),
  schedules: z.array(scheduleDefinitionSchema),
  technologies: z.array(watchedTechnologySchema),
});

const schedulePreviewSchema: z.ZodType<SchedulePreview> = z.object({
  candidateCount: z.number().int().nonnegative(),
  explanation: z.string().min(1),
  externalDelivery: z.boolean(),
  localDate: z.iso.date(),
  maximumItems: z.number().int().positive(),
  nextRunAt: z.iso.datetime({ offset: true }),
  scheduleId: z.uuid(),
  scheduleType: z.string().min(1),
});

const scheduleActionResultSchema: z.ZodType<ScheduleActionResult> = z.object({
  message: z.string().min(1),
  occurrenceId: z.uuid().optional(),
  schedule: scheduleDefinitionSchema,
});

const operationsSnapshotSchema: z.ZodType<OperationsSnapshot> = z.object({
  deliveryAttempts: z.number().int().nonnegative(),
  deployment: z.object({
    environment: z.string().min(1),
    gitSha: z.string().min(1),
    version: z.string().min(1),
  }),
  generatedAt: z.iso.datetime({ offset: true }),
  occurrences: z.array(
    z.object({
      completedAt: optionalTimeSchema,
      errorCode: z.string().optional(),
      id: z.uuid(),
      idempotencyKey: z.string().min(1),
      localDate: z.iso.date(),
      scheduleId: z.uuid(),
      scheduleType: z.string().min(1),
      scheduledFor: z.iso.datetime({ offset: true }),
      startedAt: optionalTimeSchema,
      state: z.string().min(1),
      triggerType: z.string().min(1),
    }),
  ),
  oldestOverdueOccurrence: optionalTimeSchema,
  openaiBackgroundPending: z.number().int().nonnegative(),
  queues: z.array(
    z.object({
      available: z.number().int().nonnegative(),
      cancelled: z.number().int().nonnegative(),
      completed: z.number().int().nonnegative(),
      discarded: z.number().int().nonnegative(),
      queue: z.string().min(1),
      retryable: z.number().int().nonnegative(),
      running: z.number().int().nonnegative(),
      scheduled: z.number().int().nonnegative(),
    }),
  ),
  restore: z.object({ explanation: z.string().min(1), state: z.string().min(1) }),
  schedulerLastSuccess: optionalTimeSchema,
  schedules: z.array(scheduleDefinitionSchema),
  sourceErrorBudgets: z.array(
    z.object({
      attempts: z.number().int().nonnegative(),
      failureRate: z.number().min(0).max(1),
      failures: z.number().int().nonnegative(),
      sourceId: z.string().min(1),
      sourceName: z.string().min(1),
      state: errorBudgetStateSchema,
    }),
  ),
  stuckJobs: z.array(
    z.object({
      attempt: z.number().int().nonnegative(),
      attemptedAt: optionalTimeSchema,
      id: z.number().int().positive(),
      kind: z.string().min(1),
      queue: z.string().min(1),
    }),
  ),
});

const radarStateSchema = z.enum(["adopt", "trial", "assess", "hold", "reject"]);
const radarEvidenceLinkSchema = z.object({
  label: z.string().min(1),
  sourceTier: sourceTierSchema,
  url: z.url(),
});
const radarDimensionSchema = z.object({
  candidate: z.string(),
  current: z.string(),
  evidence: z.array(radarEvidenceLinkSchema),
  name: z.enum([
    "Capability",
    "Stability",
    "Maintenance",
    "Security",
    "Performance",
    "Migration",
    "Reversibility",
  ]),
  verdict: z.enum(["needs-evidence", "supported"]),
});
const radarMetricSchema = z.object({
  bundleSizeBytes: z.number().int().nonnegative().optional(),
  contributorCount: z.number().int().nonnegative(),
  id: z.uuid(),
  license: z.string().min(1),
  maintenance: z.record(z.string(), z.unknown()),
  observedAt: z.iso.datetime({ offset: true }),
  popularity: z.record(z.string(), z.unknown()),
  provenance: z.record(z.string(), z.unknown()),
  releaseVersion: z.string().min(1),
  runtimeCompatibility: z.array(z.string().min(1)),
  security: z.record(z.string(), z.unknown()),
  typesSupported: z.boolean(),
});
const radarComparisonSchema = z.object({
  assessedAt: z.iso.datetime({ offset: true }),
  confidence: z.number().min(0).max(1),
  dimensions: z.array(radarDimensionSchema).length(7),
  evidence: z.array(radarEvidenceLinkSchema),
  id: z.uuid(),
  misleading: z.boolean(),
  suggestedState: z.enum(["assess", "hold", "reject"]),
});
const radarDecisionSchema = z.object({
  applicableProjectTypes: z.array(z.string().min(1)),
  compatibilityRequirements: z.array(z.string().min(1)),
  decidedAt: z.iso.datetime({ offset: true }),
  decisionSource: z.enum(["owner", "system"]),
  evidence: z.record(z.string(), z.unknown()),
  exitConditions: z.array(z.string().min(1)),
  id: z.uuid(),
  rationale: z.string().min(3),
  reviewAt: z.iso.datetime({ offset: true }),
  state: radarStateSchema,
});
const radarCandidateSchema: z.ZodType<RadarCandidate> = z.object({
  currentState: radarStateSchema,
  decisions: z.array(radarDecisionSchema),
  discoveredAt: z.iso.datetime({ offset: true }),
  discoverySource: z.string().min(1),
  ecosystem: z.enum(["go", "jvm", "npm", "other", "python", "rust"]),
  id: z.uuid(),
  incumbentPackage: z.string().min(1),
  latestComparison: radarComparisonSchema.optional(),
  latestMetric: radarMetricSchema.optional(),
  packageName: z.string().min(1),
  repositoryUrl: z.url(),
  reviewAt: z.iso.datetime({ offset: true }),
  version: z.number().int().positive(),
});
const radarDiscoveryRunSchema: z.ZodType<RadarDiscoveryRun> = z.object({
  candidateCount: z.number().int().nonnegative(),
  completedAt: optionalTimeSchema,
  errorCode: z.string().optional(),
  evidenceCount: z.number().int().nonnegative(),
  id: z.uuid(),
  misleadingCount: z.number().int().nonnegative(),
  requestedAt: z.iso.datetime({ offset: true }),
  startedAt: optionalTimeSchema,
  state: z.enum(["completed", "failed", "queued", "running"]),
  triggerType: z.enum(["owner", "scheduled"]),
});
const radarSnapshotSchema: z.ZodType<RadarSnapshot> = z.object({
  candidates: z.array(radarCandidateSchema),
  generatedAt: z.iso.datetime({ offset: true }),
  reviewDue: z.number().int().nonnegative(),
  runs: z.array(radarDiscoveryRunSchema),
  states: z.array(radarStateSchema).length(5),
});

export const parseSourcesSnapshot = (value: unknown): SourcesSnapshot =>
  sourcesSnapshotSchema.parse(value);

export const parseManagedSource = (value: unknown): ManagedSource =>
  managedSourceSchema.parse(value);

export const parseSourcePreference = (value: unknown): SourcePreference =>
  sourcePreferenceSchema.parse(value);

export const parseSettingsSnapshot = (value: unknown): SettingsSnapshot =>
  settingsSnapshotSchema.parse(value);

export const parseScheduleDefinition = (value: unknown): ScheduleDefinition =>
  scheduleDefinitionSchema.parse(value);

export const parseSchedulePreview = (value: unknown): SchedulePreview =>
  schedulePreviewSchema.parse(value);

export const parseScheduleActionResult = (value: unknown): ScheduleActionResult =>
  scheduleActionResultSchema.parse(value);

export const parseOperationsSnapshot = (value: unknown): OperationsSnapshot =>
  operationsSnapshotSchema.parse(value);

export const parseRadarSnapshot = (value: unknown): RadarSnapshot =>
  radarSnapshotSchema.parse(value);

export const parseRadarCandidate = (value: unknown): RadarCandidate =>
  radarCandidateSchema.parse(value);

export const parseRadarDiscoveryRun = (value: unknown): RadarDiscoveryRun =>
  radarDiscoveryRunSchema.parse(value);
