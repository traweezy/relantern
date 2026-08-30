import { z } from "zod";

export const manualCaptureSchema = z
  .object({
    idempotencyKey: z.string().min(16).max(200),
    url: z.url().max(4096),
  })
  .strict();

export const opmlPreviewSchema = z.object({ opml: z.string().min(1).max(2_097_152) }).strict();

export const opmlCommitSchema = z
  .object({
    approvedIds: z.array(z.string().min(1).max(64)).min(1).max(500),
    previewId: z.uuid(),
  })
  .strict();

export const markdownExportSchema = z
  .object({ storyIds: z.array(z.uuid()).min(1).max(100) })
  .strict();

export const saveSearchSchema = z
  .object({
    filters: z
      .object({
        action: z.string().max(120).optional(),
        after: z.string().max(35).optional(),
        before: z.string().max(35).optional(),
        lifecycle: z.string().max(32).optional(),
        radar: z.string().max(16).optional(),
        saved: z.enum(["true", "false"]).optional(),
        sourceTier: z.enum(["T0", "T1", "T2", "T3"]).optional(),
        topic: z.string().max(100).optional(),
      })
      .strict(),
    name: z.string().trim().min(1).max(100),
    query: z.string().trim().min(2).max(500),
  })
  .strict();

export type ManualCaptureCommand = z.infer<typeof manualCaptureSchema>;
export type OPMLPreviewCommand = z.infer<typeof opmlPreviewSchema>;
export type OPMLCommitCommand = z.infer<typeof opmlCommitSchema>;
export type MarkdownExportCommand = z.infer<typeof markdownExportSchema>;
export type SaveSearchCommand = z.infer<typeof saveSearchSchema>;
