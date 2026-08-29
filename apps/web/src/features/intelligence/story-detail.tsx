import type { StoryDetail as StoryDetailModel } from "@relantern/domain";
import type { Route } from "next";
import Link from "next/link";
import { memo } from "react";
import { ReadingProgress } from "./reading-progress";

type StoryDetailProps = Readonly<{
  relatedHrefPrefix?: string;
  story: StoryDetailModel;
  timezone?: string;
}>;

const formatDate = (timestamp: string, timezone: string): string =>
  new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone,
  }).format(new Date(timestamp));

const StoryDetailComponent = ({
  relatedHrefPrefix = "/story",
  story,
  timezone = "UTC",
}: StoryDetailProps) => (
  <article className="story-reader">
    <ReadingProgress />
    <header className="story-reader-header">
      <div className="story-reader-kicker">
        <span className={`signal-badge signal-${story.signal}`}>{story.signal}</span>
        <span>{story.sourceTier} primary evidence</span>
        <span>{story.readTimeMinutes} min read</span>
      </div>
      <h1>{story.headline}</h1>
      <p className="story-reader-summary">{story.summary}</p>
      <div className="story-reader-timeline">
        <div>
          <span>First observed</span>
          <time dateTime={story.firstSeenAt}>{formatDate(story.firstSeenAt, timezone)}</time>
        </div>
        <div>
          <span>Last changed</span>
          <time dateTime={story.lastChangedAt}>{formatDate(story.lastChangedAt, timezone)}</time>
        </div>
        <div>
          <span>Confidence</span>
          <strong>{story.confidence}</strong>
        </div>
      </div>
    </header>

    <section aria-labelledby="brief-section-title" className="reader-section reader-brief">
      <p className="eyebrow">Executive brief</p>
      <h2 id="brief-section-title">What changed, and why it matters</h2>
      <p>{story.whyItMatters}</p>
      <div className="action-callout">
        <span>Recommended next action</span>
        <p>{story.recommendedAction}</p>
      </div>
    </section>

    <section aria-labelledby="evidence-section-title" className="reader-section">
      <div className="reader-section-heading">
        <div>
          <p className="eyebrow">Claim → evidence</p>
          <h2 id="evidence-section-title">Material assertions</h2>
        </div>
        <span>{story.assertions.length} checked</span>
      </div>
      {story.assertions.length === 0 ? (
        <p className="reader-muted">No publishable assertions are available for this brief.</p>
      ) : (
        <ol className="claim-list">
          {story.assertions.map((assertion, index) => (
            <li key={assertion.claim}>
              <div className="claim-index">{String(index + 1).padStart(2, "0")}</div>
              <div>
                <p className="claim-copy">{assertion.claim}</p>
                <div className="claim-sources">
                  {assertion.sources.map((source) => (
                    <a href={source.url} key={source.url} rel="noreferrer" target="_blank">
                      {source.label} <span>{source.tier}</span>
                    </a>
                  ))}
                </div>
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>

    <section aria-labelledby="sources-section-title" className="reader-section">
      <div className="reader-section-heading">
        <div>
          <p className="eyebrow">Provenance</p>
          <h2 id="sources-section-title">Primary source set</h2>
        </div>
        <span>{story.sources.length} sources</span>
      </div>
      <ul className="source-list">
        {story.sources.map((source) => (
          <li key={source.url}>
            <div>
              <strong>{source.label}</strong>
              <span>{source.domain}</span>
            </div>
            <a href={source.url} rel="noreferrer" target="_blank">
              Open source <span aria-hidden="true">↗</span>
            </a>
          </li>
        ))}
      </ul>
    </section>

    {story.uncertainties.length > 0 && (
      <section aria-labelledby="uncertainties-title" className="reader-section uncertainty-section">
        <p className="eyebrow">Review boundary</p>
        <h2 id="uncertainties-title">What remains uncertain</h2>
        <ul>
          {story.uncertainties.map((uncertainty) => (
            <li key={uncertainty}>{uncertainty}</li>
          ))}
        </ul>
      </section>
    )}

    {story.related.length > 0 && (
      <section aria-labelledby="related-title" className="reader-section">
        <p className="eyebrow">Continue the thread</p>
        <h2 id="related-title">Related intelligence</h2>
        <div className="related-list">
          {story.related.map((related) => (
            <Link href={`${relatedHrefPrefix}/${related.id}` as Route} key={related.id}>
              <span>{related.signal}</span>
              <strong>{related.headline}</strong>
              <small>{related.readTimeMinutes} min</small>
            </Link>
          ))}
        </div>
      </section>
    )}
  </article>
);

export const StoryDetail = memo<StoryDetailProps>(StoryDetailComponent);
StoryDetail.displayName = "StoryDetail";
