import type {
  DigestCandidate,
  DigestPayload,
  DigestRecord,
  DigestSnapshot,
} from "@relantern/domain";
import { z } from "zod";

const channelSchema = z.enum(["dashboard", "discord", "email"]);
const timeSchema = z.iso.datetime({ offset: true });

const renderedItemSchema = z.object({
  action: z.string(),
  appPath: z.string().startsWith("/"),
  headline: z.string().min(1),
  id: z.uuid(),
  reason: z.string().min(1),
  score: z.number().min(0).max(1),
  signal: z.string().min(1),
  sourceUrl: z.url(),
  summary: z.string().min(1),
  type: z.enum(["radar", "story"]),
});

const candidateSchema: z.ZodType<DigestCandidate> = renderedItemSchema.extend({
  category: z.enum(["coming_soon", "general", "later", "radar", "release", "security"]),
  evidence: z.record(z.string(), z.unknown()),
  itemId: z.uuid().optional(),
  observedAt: timeSchema,
  radarId: z.uuid().optional(),
  storyId: z.uuid().optional(),
});

const payloadSchema: z.ZodType<DigestPayload> = z.object({
  channel: channelSchema,
  executiveSummary: z.string().min(1),
  generatedAt: timeSchema,
  items: z.array(renderedItemSchema),
  localDate: z.iso.date(),
  title: z.string().min(1),
  windowEnd: timeSchema,
  windowStart: timeSchema,
});

export const digestRecordSchema: z.ZodType<DigestRecord> = z.object({
  attemptCount: z.number().int().nonnegative(),
  channel: channelSchema,
  completedAt: timeSchema.optional(),
  emptyBehavior: z.enum(["all_clear", "dashboard_only", "send_nothing"]),
  errorCode: z.string().min(1).optional(),
  executiveSummary: z.string().min(1),
  generatedAt: timeSchema,
  id: z.uuid(),
  itemLimit: z.number().int().positive(),
  items: z.array(candidateSchema),
  localDate: z.iso.date(),
  minimumScore: z.number().min(0).max(1),
  occurrenceId: z.uuid(),
  providerIdempotencyKey: z.string().min(16),
  rendered: payloadSchema,
  state: z.enum(["delivered", "delivering", "failed", "ready", "skipped"]),
  windowEnd: timeSchema,
  windowStart: timeSchema,
});

const digestSnapshotSchema: z.ZodType<DigestSnapshot> = z.object({
  digests: z.array(digestRecordSchema),
  generatedAt: timeSchema,
});

export const parseDigestRecord = (value: unknown): DigestRecord => digestRecordSchema.parse(value);

export const parseDigestSnapshot = (value: unknown): DigestSnapshot =>
  digestSnapshotSchema.parse(value);
