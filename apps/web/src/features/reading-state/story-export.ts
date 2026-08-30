import type { StoryAnnotation, StoryDetail, StoryReadingState, StoryTag } from "@relantern/domain";

type StoryMarkdownInput = Readonly<{
  annotations: readonly StoryAnnotation[];
  exportedAt: string;
  state: StoryReadingState;
  story: StoryDetail;
  tags: readonly StoryTag[];
}>;

const cleanInline = (value: string): string => value.replaceAll("\n", " ").trim();

const annotationMarkdown = (annotation: StoryAnnotation, story: StoryDetail): string => {
  const heading = annotation.type === "document_note" ? "Note" : "Highlight";
  const lines = [`### ${heading}`];
  if (!annotation.orphaned && annotation.startOffset !== null && annotation.endOffset !== null) {
    const quote = [...story.normalizedContent]
      .slice(annotation.startOffset, annotation.endOffset)
      .join("")
      .trim();
    if (quote !== "") {
      lines.push("", ...quote.split("\n").map((line) => `> ${line}`));
    }
  }
  if (annotation.body !== "") {
    lines.push("", annotation.body.trim());
  }
  const location =
    annotation.startOffset === null || annotation.endOffset === null
      ? "document"
      : `characters ${annotation.startOffset}–${annotation.endOffset}`;
  lines.push(
    "",
    `- Revision: \`${annotation.revisionId}\``,
    `- Location: ${location}`,
    `- Source status: ${annotation.orphaned ? "changed; original revision retained" : "current"}`,
  );
  return lines.join("\n");
};

export const buildStoryMarkdown = ({
  annotations,
  exportedAt,
  state,
  story,
  tags,
}: StoryMarkdownInput): string => {
  const selectedTags = tags
    .filter((tag) => state.tagIds.includes(tag.id))
    .map((tag) => cleanInline(tag.name));
  const sections = [
    `# ${cleanInline(story.headline)}`,
    [
      `- Story ID: \`${story.id}\``,
      `- Revision: \`${story.revisionId}\``,
      `- Exported: ${exportedAt}`,
      `- Reading state: ${state.isRead ? "read" : "unread"}, ${state.location}`,
      `- Tags: ${selectedTags.length === 0 ? "none" : selectedTags.join(", ")}`,
    ].join("\n"),
    `## Summary\n\n${story.summary.trim()}`,
    `## Why it matters\n\n${story.whyItMatters.trim()}`,
    `## Recommended action\n\n${story.recommendedAction.trim()}`,
    `## Material assertions\n\n${
      story.assertions.length === 0
        ? "No publishable assertions."
        : story.assertions
            .map((assertion) => {
              const sources = assertion.sources
                .map(
                  (source) => `  - [${cleanInline(source.label)}](${source.url}) — ${source.tier}`,
                )
                .join("\n");
              return `- ${assertion.claim.trim()}\n${sources}`;
            })
            .join("\n")
    }`,
    `## Sources\n\n${story.sources
      .map((source) => `- [${cleanInline(source.label)}](${source.url}) — ${source.tier}`)
      .join("\n")}`,
    `## Notes and highlights\n\n${
      annotations.length === 0
        ? "No private annotations."
        : annotations.map((annotation) => annotationMarkdown(annotation, story)).join("\n\n")
    }`,
  ];
  return `${sections.join("\n\n")}\n`;
};
