import type { StorySummary } from "@relantern/domain";
import type { Route } from "next";
import Link from "next/link";
import { memo } from "react";

type StoryCardProps = Readonly<{
  href: string;
  story: StorySummary;
  timezone?: string;
}>;

const signalLabels = {
  "breaking-change": "Breaking change",
  deprecation: "Deprecation",
  general: "Engineering",
  release: "Release",
  security: "Security",
} as const;

const formatTime = (timestamp: string, timezone: string): string =>
  new Intl.DateTimeFormat("en-US", {
    hour: "numeric",
    minute: "2-digit",
    timeZone: timezone,
  }).format(new Date(timestamp));

const StoryCardComponent = ({ href, story, timezone = "UTC" }: StoryCardProps) => (
  <article className="story-card">
    <div className="story-meta-row">
      <div className="story-badges">
        <span className={`signal-badge signal-${story.signal}`}>{signalLabels[story.signal]}</span>
        <span className="source-tier">{story.sourceTier}</span>
        {story.status === "updated" && <span className="updated-badge">Updated</span>}
      </div>
      <time dateTime={story.lastChangedAt}>{formatTime(story.lastChangedAt, timezone)}</time>
    </div>
    <h2>
      <Link href={href as Route}>{story.headline}</Link>
    </h2>
    <p className="story-summary">{story.summary}</p>
    <div className="story-why">
      <span>Why it matters</span>
      <p>{story.whyItMatters}</p>
    </div>
    <div className="story-footer">
      <p>
        <span>Action</span> {story.recommendedAction}
      </p>
      <div className="story-metrics">
        <span>{story.confidence} confidence</span>
        <span>{story.sourceCount} sources</span>
        <span>{story.readTimeMinutes} min</span>
      </div>
    </div>
  </article>
);

export const StoryCard = memo<StoryCardProps>(StoryCardComponent);
StoryCard.displayName = "StoryCard";
