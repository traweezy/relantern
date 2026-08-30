import type {
  ImportCandidate,
  ImportCommitResult,
  ImportPreview,
  IntelligenceSearchResponse,
  IntelligenceSearchResult,
  ManualCapture,
  ReleaseCatalog,
  ReleaseEntry,
  SavedIntelligenceSearch,
  TechnologyRelease,
} from "@relantern/domain";
import { z } from "zod";

const sourceTierSchema = z.enum(["T0", "T1", "T2", "T3"]);
const storySignalSchema = z.enum([
  "breaking-change",
  "deprecation",
  "general",
  "release",
  "security",
]);
const lifecycleSchema = z.enum([
  "deprecated",
  "eol",
  "preview",
  "release_candidate",
  "stable",
  "unknown",
]);
const nullableRankSchema = z.number().int().positive().nullable();
const nullableSimilaritySchema = z.number().min(-1).max(1).nullable();

const searchResultSchema: z.ZodType<IntelligenceSearchResult> = z.object({
  explanation: z.object({
    keywordRank: nullableRankSchema,
    semanticRank: nullableRankSchema,
    semanticSimilarity: nullableSimilaritySchema,
    summary: z.string().min(1),
  }),
  firstSeenAt: z.iso.datetime({ offset: true }),
  itemId: z.uuid(),
  lifecycleState: lifecycleSchema,
  packageName: z.string(),
  recommendedAction: z.string(),
  saved: z.boolean(),
  score: z.number().nonnegative(),
  signal: storySignalSchema,
  sourceTier: sourceTierSchema,
  storyId: z.uuid(),
  summary: z.string(),
  title: z.string().min(1),
});

const searchResponseSchema: z.ZodType<IntelligenceSearchResponse> = z.object({
  explanation: z.string().min(1),
  query: z.string().min(2),
  resultCount: z.number().int().nonnegative(),
  results: z.array(searchResultSchema),
});

const searchFiltersSchema = z.object({
  action: z.string().optional(),
  after: z.string().optional(),
  before: z.string().optional(),
  lifecycle: z.string().optional(),
  radar: z.string().optional(),
  saved: z.string().optional(),
  sourceTier: z.string().optional(),
  topic: z.string().optional(),
});

const savedSearchSchema: z.ZodType<SavedIntelligenceSearch> = z.object({
  createdAt: z.iso.datetime({ offset: true }),
  filters: searchFiltersSchema,
  id: z.uuid(),
  name: z.string().min(1),
  query: z.string().min(2),
  updatedAt: z.iso.datetime({ offset: true }),
});

const savedSearchesSchema = z.object({ searches: z.array(savedSearchSchema) });

const releaseEntrySchema: z.ZodType<ReleaseEntry> = z.object({
  headline: z.string().min(1),
  lastVerifiedAt: z.iso.datetime({ offset: true }),
  packageName: z.string(),
  sourceTier: sourceTierSchema,
  sourceUrl: z.url(),
  state: z.enum([
    "deprecation",
    "deprecated",
    "eol",
    "preview",
    "release_candidate",
    "security",
    "stable",
    "unknown",
  ]),
  storyId: z.uuid(),
  summary: z.string(),
  targetDate: z.iso.datetime({ offset: true }).nullable(),
  technology: z.string().min(1),
  version: z.string(),
});

const technologyReleaseSchema: z.ZodType<TechnologyRelease> = z.object({
  currentVersion: z.string(),
  entries: z.array(releaseEntrySchema),
  newestVersion: z.string(),
  packageName: z.string(),
  technology: z.string().min(1),
  upgradeStatus: z.enum(["current", "not-configured", "update-available"]),
});

const releaseCatalogSchema: z.ZodType<ReleaseCatalog> = z.object({
  comingSoon: z.array(releaseEntrySchema),
  generatedAt: z.iso.datetime({ offset: true }),
  technologies: z.array(technologyReleaseSchema),
});

const manualCaptureResponseSchema: z.ZodType<ManualCapture> = z.object({
  completedAt: z.iso.datetime({ offset: true }).optional(),
  createdAt: z.iso.datetime({ offset: true }),
  errorCode: z.string().optional(),
  id: z.uuid(),
  state: z.enum(["completed", "deduplicating", "failed", "fetching", "parsing", "queued"]),
  storyId: z.uuid().optional(),
  url: z.url(),
});

const importCandidateSchema: z.ZodType<ImportCandidate> = z.object({
  connector: z.string(),
  duplicate: z.boolean(),
  explanation: z.string().min(1),
  id: z.string().min(1),
  name: z.string().min(1),
  url: z.string(),
  valid: z.boolean(),
});

const importPreviewResponseSchema: z.ZodType<ImportPreview> = z.object({
  candidates: z.array(importCandidateSchema),
  expiresAt: z.iso.datetime({ offset: true }),
  id: z.uuid(),
});

const importCommitResponseSchema: z.ZodType<ImportCommitResult> = z.object({
  importedCount: z.number().int().nonnegative(),
  pendingSourceIds: z.array(z.string().min(1)),
});

export const parseSearchResponse = (value: unknown): IntelligenceSearchResponse =>
  searchResponseSchema.parse(value);

export const parseSavedSearch = (value: unknown): SavedIntelligenceSearch =>
  savedSearchSchema.parse(value);

export const parseSavedSearches = (value: unknown): readonly SavedIntelligenceSearch[] =>
  savedSearchesSchema.parse(value).searches;

export const parseReleaseCatalog = (value: unknown): ReleaseCatalog =>
  releaseCatalogSchema.parse(value);

export const parseManualCapture = (value: unknown): ManualCapture =>
  manualCaptureResponseSchema.parse(value);

export const parseImportPreview = (value: unknown): ImportPreview =>
  importPreviewResponseSchema.parse(value);

export const parseImportCommit = (value: unknown): ImportCommitResult =>
  importCommitResponseSchema.parse(value);
