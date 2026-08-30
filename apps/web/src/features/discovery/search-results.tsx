import type { IntelligenceSearchResponse } from "@relantern/domain";
import type { Route } from "next";
import Link from "next/link";
import { memo } from "react";

type SearchResultsProps = Readonly<{
  response: IntelligenceSearchResponse;
  timezone: string;
}>;

const formatObservedAt = (value: string, timezone: string): string =>
  new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeZone: timezone,
  }).format(new Date(value));

const SearchResultsComponent = ({ response, timezone }: SearchResultsProps) => (
  <section aria-labelledby="search-results-title" className="search-results">
    <div className="search-result-summary">
      <div>
        <p className="eyebrow">Hybrid retrieval</p>
        <h2 id="search-results-title">
          {response.resultCount} result{response.resultCount === 1 ? "" : "s"}
        </h2>
      </div>
      <p>{response.explanation}</p>
    </div>
    {response.results.length === 0 ? (
      <div className="reading-empty-state">
        <p className="eyebrow">No exact or semantic match</p>
        <h2>Try widening the evidence window.</h2>
        <p>Remove a filter, check the package spelling, or search by the underlying concept.</p>
      </div>
    ) : (
      <ol className="search-result-list">
        {response.results.map((result) => (
          <li key={result.storyId}>
            <article className="search-result-card">
              <div className="search-result-rank">
                <span className="sr-only">Fused score </span>
                {result.score.toFixed(3)}
              </div>
              <div>
                <div className="story-badges">
                  <span className={`signal-badge signal-${result.signal}`}>{result.signal}</span>
                  <span className="source-tier">{result.sourceTier}</span>
                  <span className="source-tier">{result.lifecycleState.replace("_", " ")}</span>
                  {result.saved && <span className="updated-badge">Saved</span>}
                </div>
                <h3>
                  <Link href={`/story/${result.storyId}` as Route}>{result.title}</Link>
                </h3>
                <p>{result.summary}</p>
                <dl className="search-evidence-grid">
                  <div>
                    <dt>Matched by</dt>
                    <dd>{result.explanation.summary}</dd>
                  </div>
                  <div>
                    <dt>Package</dt>
                    <dd>{result.packageName || "Concept match"}</dd>
                  </div>
                  <div>
                    <dt>Observed</dt>
                    <dd>{formatObservedAt(result.firstSeenAt, timezone)}</dd>
                  </div>
                  <div>
                    <dt>Action</dt>
                    <dd>{result.recommendedAction || "Review evidence"}</dd>
                  </div>
                </dl>
              </div>
            </article>
          </li>
        ))}
      </ol>
    )}
  </section>
);

export const SearchResults = memo<SearchResultsProps>(SearchResultsComponent);
SearchResults.displayName = "SearchResults";
