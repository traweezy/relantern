import type { ReleaseCatalog as ReleaseCatalogModel, ReleaseEntry } from "@relantern/domain";
import type { Route } from "next";
import Link from "next/link";
import { memo } from "react";

type ReleaseCatalogProps = Readonly<{
  catalog: ReleaseCatalogModel;
  timezone: string;
}>;

const formatDate = (value: string, timezone: string): string =>
  new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeZone: timezone,
  }).format(new Date(value));

type ComingSoonCardProps = Readonly<{
  entry: ReleaseEntry;
  timezone: string;
}>;

const ComingSoonCardComponent = ({ entry, timezone }: ComingSoonCardProps) => (
  <article className="coming-soon-card">
    <div className="story-badges">
      <span className="signal-badge">{entry.state.replace("_", " ")}</span>
      <span className="source-tier">{entry.sourceTier}</span>
    </div>
    <h3>{entry.headline}</h3>
    <p>{entry.summary}</p>
    <dl>
      <div>
        <dt>Source target</dt>
        <dd>
          {entry.targetDate === null ? "No date declared" : formatDate(entry.targetDate, timezone)}
        </dd>
      </div>
      <div>
        <dt>Last verified</dt>
        <dd>{formatDate(entry.lastVerifiedAt, timezone)}</dd>
      </div>
    </dl>
    <div className="release-card-links">
      <Link href={`/story/${entry.storyId}` as Route}>Open brief</Link>
      <a href={entry.sourceUrl} rel="noreferrer" target="_blank">
        Verify source
      </a>
    </div>
  </article>
);

const ComingSoonCard = memo<ComingSoonCardProps>(ComingSoonCardComponent);
ComingSoonCard.displayName = "ComingSoonCard";

const ReleaseCatalogComponent = ({ catalog, timezone }: ReleaseCatalogProps) => (
  <>
    <section aria-labelledby="release-matrix-title" className="release-section">
      <div className="collection-toolbar">
        <div>
          <p className="eyebrow">Verified versions</p>
          <h2 id="release-matrix-title">Technology matrix</h2>
        </div>
        <p>Stable, security, and deprecation evidence from T0/T1 sources only.</p>
      </div>
      {catalog.technologies.length === 0 ? (
        <div className="reading-empty-state">
          <p className="eyebrow">No verified release evidence</p>
          <h2>The matrix is waiting for source-of-record stories.</h2>
          <p>Items appear after extraction and evidence validation complete.</p>
        </div>
      ) : (
        <div className="release-table-wrap">
          <table className="release-table">
            <caption>Watched and newest verified technology versions</caption>
            <thead>
              <tr>
                <th scope="col">Technology</th>
                <th scope="col">Watched</th>
                <th scope="col">Newest</th>
                <th scope="col">Status</th>
                <th scope="col">Verified evidence</th>
              </tr>
            </thead>
            <tbody>
              {catalog.technologies.map((technology) => (
                <tr key={`${technology.technology}:${technology.packageName}`}>
                  <th scope="row">
                    {technology.technology}
                    <small>{technology.packageName}</small>
                  </th>
                  <td>{technology.currentVersion || "Not set"}</td>
                  <td>{technology.newestVersion || "Unknown"}</td>
                  <td>
                    <span className={`upgrade-status upgrade-${technology.upgradeStatus}`}>
                      {technology.upgradeStatus.replace("-", " ")}
                    </span>
                  </td>
                  <td>{technology.entries.length}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>

    <section aria-labelledby="coming-soon-title" className="release-section">
      <div className="collection-toolbar">
        <div>
          <p className="eyebrow">Source-declared horizon</p>
          <h2 id="coming-soon-title">Coming Soon</h2>
        </div>
        <p>No model-predicted dates. Every timeline is tied to its last source verification.</p>
      </div>
      {catalog.comingSoon.length === 0 ? (
        <div className="reading-empty-state">
          <p className="eyebrow">Horizon clear</p>
          <h2>No verified previews or deadlines yet.</h2>
          <p>
            Official proposals, release candidates, and support-window changes will collect here.
          </p>
        </div>
      ) : (
        <div className="coming-soon-grid">
          {catalog.comingSoon.map((entry) => (
            <ComingSoonCard entry={entry} key={entry.storyId} timezone={timezone} />
          ))}
        </div>
      )}
    </section>
  </>
);

export const ReleaseCatalog = memo<ReleaseCatalogProps>(ReleaseCatalogComponent);
ReleaseCatalog.displayName = "ReleaseCatalog";
