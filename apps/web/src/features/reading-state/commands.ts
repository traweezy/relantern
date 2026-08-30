import { z } from "zod";

const uuidSchema = z.uuid();
const idempotencyKeySchema = z.string().trim().min(16).max(200);
const dismissalReasonSchema = z.enum([
  "already_known",
  "duplicate",
  "irrelevant_topic",
  "low_quality",
  "too_promotional",
]);

export const dismissalFormSchema = z.object({ reason: dismissalReasonSchema }).strict();

export const customSnoozeFormSchema = z
  .object({ until: z.string().regex(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/) })
  .strict();

export const readingActionSchema = z.enum([
  "add_tag",
  "already_known",
  "archive",
  "dismiss",
  "mark_read",
  "mark_unread",
  "move_inbox",
  "move_later",
  "remove_tag",
  "snooze",
  "star",
  "unstar",
  "unsnooze",
  "update_progress",
]);

export const readingMutationSchema = z
  .object({
    action: readingActionSchema,
    dismissedReason: dismissalReasonSchema.optional(),
    idempotencyKey: idempotencyKeySchema,
    lastParagraphId: z.string().trim().min(1).max(255).optional(),
    readingProgress: z.number().min(0).max(1).optional(),
    snoozedUntil: z.iso.datetime({ offset: true }).optional(),
    tagId: uuidSchema.optional(),
    version: z.number().int().min(0),
  })
  .strict()
  .superRefine((command, context) => {
    if (command.action === "snooze" && command.snoozedUntil === undefined) {
      context.addIssue({ code: "custom", message: "Snooze requires a return time." });
    }
    if (
      (command.action === "add_tag" || command.action === "remove_tag") &&
      command.tagId === undefined
    ) {
      context.addIssue({ code: "custom", message: "Tag commands require a tag ID." });
    }
    if (command.action === "update_progress" && command.readingProgress === undefined) {
      context.addIssue({ code: "custom", message: "Progress commands require a progress value." });
    }
  });

const bulkItemSchema = z
  .object({
    storyId: uuidSchema,
    version: z.number().int().min(0),
  })
  .strict();

export const bulkReadingMutationSchema = z
  .object({
    action: readingActionSchema,
    dismissedReason: dismissalReasonSchema.optional(),
    expectedCount: z.number().int().min(1).max(200),
    idempotencyKey: idempotencyKeySchema,
    items: z.array(bulkItemSchema).min(1).max(200),
    snoozedUntil: z.iso.datetime({ offset: true }).optional(),
    tagId: uuidSchema.optional(),
  })
  .strict()
  .superRefine((command, context) => {
    if (command.expectedCount !== command.items.length) {
      context.addIssue({
        code: "custom",
        message: "Confirmed count does not match the selection.",
      });
    }
    if (new Set(command.items.map((item) => item.storyId)).size !== command.items.length) {
      context.addIssue({ code: "custom", message: "Bulk selection contains duplicate stories." });
    }
    if (command.action === "snooze" && command.snoozedUntil === undefined) {
      context.addIssue({ code: "custom", message: "Snooze requires a return time." });
    }
    if (
      (command.action === "add_tag" || command.action === "remove_tag") &&
      command.tagId === undefined
    ) {
      context.addIssue({ code: "custom", message: "Tag commands require a tag ID." });
    }
  });

export const laterOrderSchema = z
  .object({
    expectedCount: z.number().int().min(1).max(200),
    idempotencyKey: idempotencyKeySchema,
    items: z.array(bulkItemSchema).min(1).max(200),
  })
  .strict()
  .superRefine((command, context) => {
    if (command.expectedCount !== command.items.length) {
      context.addIssue({ code: "custom", message: "Confirmed count does not match the ordering." });
    }
    if (new Set(command.items.map((item) => item.storyId)).size !== command.items.length) {
      context.addIssue({ code: "custom", message: "Later ordering contains duplicate stories." });
    }
  });

export const undoSchema = z.object({ mutationId: uuidSchema }).strict();
export const bulkUndoSchema = z.object({ bulkId: uuidSchema }).strict();

export const tagInputSchema = z
  .object({
    colorToken: z.enum(["accent", "blue", "green", "orange", "purple", "red", "slate"]),
    name: z.string().trim().min(1).max(80),
  })
  .strict();

export const feedbackCommandSchema = z
  .object({
    idempotencyKey: idempotencyKeySchema,
    note: z.string().trim().max(2_000).optional(),
    type: z.enum([
      "useful",
      "already_known",
      "irrelevant",
      "too_shallow",
      "too_verbose",
      "incorrect",
    ]),
  })
  .strict();

export const annotationInputSchema = z
  .object({
    body: z.string().trim().max(20_000),
    endOffset: z.number().int().min(1).max(10_000_000).optional(),
    quoteHash: z
      .string()
      .regex(/^[0-9a-f]{64}$/)
      .optional(),
    revisionId: uuidSchema.optional(),
    startOffset: z.number().int().min(0).max(9_999_999).optional(),
    type: z.enum(["document_note", "highlight", "highlight_note"]),
  })
  .strict()
  .superRefine((annotation, context) => {
    if (annotation.type === "document_note" && annotation.body.length === 0) {
      context.addIssue({ code: "custom", message: "Document notes require a body." });
    }
    if (annotation.type !== "document_note") {
      if (
        annotation.startOffset === undefined ||
        annotation.endOffset === undefined ||
        annotation.endOffset <= annotation.startOffset ||
        annotation.quoteHash === undefined
      ) {
        context.addIssue({
          code: "custom",
          message: "Highlights require offsets and a quote hash.",
        });
      }
      if (annotation.type === "highlight_note" && annotation.body.length === 0) {
        context.addIssue({ code: "custom", message: "Highlight notes require a body." });
      }
    }
  });

export const annotationUpdateSchema = z
  .object({ body: z.string().trim().min(1).max(20_000) })
  .strict();

export const noteBodySchema = z.object({ body: z.string().trim().min(1).max(20_000) }).strict();
export const highlightBodySchema = z.object({ body: z.string().trim().max(20_000) }).strict();

export type ReadingMutationCommand = z.infer<typeof readingMutationSchema>;
export type BulkReadingMutationCommand = z.infer<typeof bulkReadingMutationSchema>;
export type LaterOrderCommand = z.infer<typeof laterOrderSchema>;
export type TagInput = z.infer<typeof tagInputSchema>;
export type FeedbackCommand = z.infer<typeof feedbackCommandSchema>;
export type AnnotationInput = z.infer<typeof annotationInputSchema>;
export type AnnotationUpdate = z.infer<typeof annotationUpdateSchema>;
