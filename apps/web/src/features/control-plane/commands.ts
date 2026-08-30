import { z } from "zod";

export const sourcePreferenceCommandSchema = z
  .object({
    excludeFromDigest: z.boolean(),
    expectedVersion: z.number().int().nonnegative(),
    muted: z.boolean(),
    relevanceAdjustment: z.number().min(-1).max(1),
  })
  .strict();

export const sourceActionCommandSchema = z
  .object({
    action: z.enum(["approve", "pause", "reject", "resume", "test"]),
    reason: z.string().trim().min(3).max(1000),
  })
  .strict();

const interestTopicCommandSchema = z.object({
  exclusions: z.array(z.string().trim().min(1).max(100)).max(50),
  keywords: z.array(z.string().trim().min(1).max(100)).max(50),
  priority: z.number().int().min(1).max(5),
  topicId: z.string().regex(/^[a-z0-9]+(?:[-_][a-z0-9]+)*$/),
  weight: z.number().min(0).max(1),
});

const watchedTechnologyCommandSchema = z.object({
  currentVersion: z.string().max(100),
  id: z.uuid().optional(),
  lastVerifiedAt: z.iso.datetime({ offset: true }).optional(),
  packageName: z.string().trim().min(1).max(255),
  source: z.string().trim().min(1).max(120),
  status: z.enum(["active", "evaluating", "legacy", "planned"]),
  technology: z.string().trim().min(1).max(120),
  versionConstraint: z.string().max(200),
});

export const settingsCommandSchema = z
  .object({
    auditRetentionDays: z.number().int().min(30).max(3650),
    criticalAlertsBypass: z.boolean(),
    expectedVersion: z.number().int().positive(),
    monthlyHardBudgetUsd: z.string().regex(/^(?:0|[1-9][0-9]{0,7})(?:\.[0-9]{1,8})?$/),
    monthlySoftBudgetUsd: z.string().regex(/^(?:0|[1-9][0-9]{0,7})(?:\.[0-9]{1,8})?$/),
    profileName: z.string().trim().min(1).max(120),
    profileSummary: z.string().trim().max(2000),
    quietHoursEnd: z.string().regex(/^\d{2}:\d{2}$/),
    quietHoursStart: z.string().regex(/^\d{2}:\d{2}$/),
    rawRetentionDays: z.number().int().min(7).max(3650),
    technologies: z.array(watchedTechnologyCommandSchema).max(100),
    timezone: z.string().trim().min(1).max(255),
    topics: z.array(interestTopicCommandSchema).min(1).max(50),
  })
  .strict();

export const scheduleCommandSchema = z
  .object({
    catchupGraceMinutes: z.number().int().min(0).max(10_080),
    catchupPolicy: z.enum(["catch_up", "skip"]),
    channels: z
      .array(z.enum(["dashboard", "discord", "email"]))
      .min(1)
      .max(3),
    daysOfWeek: z.array(z.number().int().min(1).max(7)).min(1).max(7),
    emptyBehavior: z.enum(["all_clear", "dashboard_only", "send_nothing"]),
    enabled: z.boolean(),
    expectedVersion: z.number().int().positive(),
    includeComingSoon: z.boolean(),
    includeLaterReminders: z.boolean(),
    includeRadarCandidates: z.boolean(),
    localTime: z.string().regex(/^\d{2}:\d{2}$/),
    maximumItems: z.union([z.literal(5), z.literal(10), z.literal(15), z.literal(20)]),
    minimumScore: z.number().min(0).max(1),
    timezone: z.string().trim().min(1).max(255),
    weekendMode: z.enum(["normal", "off", "weekly_only"]),
  })
  .strict();

export const scheduleActionCommandSchema = z
  .object({
    action: z.enum(["pause", "resume", "run_now", "skip_next"]),
    idempotencyKey: z.string().min(16).max(200).optional(),
    pausedUntil: z.iso.datetime({ offset: true }).optional(),
    reason: z.string().trim().min(3).max(1000),
  })
  .strict();

export type SourcePreferenceCommand = z.infer<typeof sourcePreferenceCommandSchema>;
export type SourceActionCommand = z.infer<typeof sourceActionCommandSchema>;
export type SettingsCommand = z.infer<typeof settingsCommandSchema>;
export type ScheduleCommand = z.infer<typeof scheduleCommandSchema>;
export type ScheduleActionCommand = z.infer<typeof scheduleActionCommandSchema>;
