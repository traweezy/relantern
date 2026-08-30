import type { IntelligenceSearchFilters } from "@relantern/domain";
import type { Metadata, Route } from "next";
import Link from "next/link";
import { SavedSearches } from "@/features/discovery/saved-searches";
import { SearchResults } from "@/features/discovery/search-results";
import { requireOwnerSession } from "@/server/auth/session";
import {
  getSavedSearches,
  type SearchParameters,
  searchIntelligence,
} from "@/server/discovery/client";

type SearchPageParameters = Readonly<{
  action?: string;
  after?: string;
  before?: string;
  lifecycle?: string;
  q?: string;
  radar?: string;
  saved?: string;
  sourceTier?: string;
  topic?: string;
}>;

type SearchPageProps = Readonly<{ searchParams: Promise<SearchPageParameters> }>;

export const metadata: Metadata = { title: "Search | Relantern" };

const optionalParameter = (value: string | undefined): string | undefined =>
  value === undefined || value.trim() === "" ? undefined : value.trim();

const SearchPage = async ({ searchParams }: SearchPageProps) => {
  const [owner, requested] = await Promise.all([requireOwnerSession(), searchParams]);
  const action = optionalParameter(requested.action);
  const after = optionalParameter(requested.after);
  const before = optionalParameter(requested.before);
  const lifecycle = optionalParameter(requested.lifecycle);
  const radar = optionalParameter(requested.radar);
  const saved = optionalParameter(requested.saved);
  const sourceTier = optionalParameter(requested.sourceTier);
  const topic = optionalParameter(requested.topic);
  const parameters: SearchParameters = {
    q: requested.q?.trim() ?? "",
    ...(action === undefined ? {} : { action }),
    ...(after === undefined ? {} : { after }),
    ...(before === undefined ? {} : { before }),
    ...(lifecycle === undefined ? {} : { lifecycle }),
    ...(radar === undefined ? {} : { radar }),
    ...(saved === undefined ? {} : { saved }),
    ...(sourceTier === undefined ? {} : { sourceTier }),
    ...(topic === undefined ? {} : { topic }),
  };
  const currentFilters: IntelligenceSearchFilters = {
    ...(action === undefined ? {} : { action }),
    ...(after === undefined ? {} : { after }),
    ...(before === undefined ? {} : { before }),
    ...(lifecycle === undefined ? {} : { lifecycle }),
    ...(radar === undefined ? {} : { radar }),
    ...(saved === undefined ? {} : { saved }),
    ...(sourceTier === undefined ? {} : { sourceTier }),
    ...(topic === undefined ? {} : { topic }),
  };
  const [response, savedSearches] = await Promise.all([
    parameters.q.length >= 2
      ? searchIntelligence(owner.userID, parameters).catch(() => null)
      : Promise.resolve(null),
    getSavedSearches(owner.userID).catch(() => []),
  ]);

  return (
    <>
      <header className="intelligence-header search-header">
        <div>
          <p className="eyebrow">Private corpus · exact + semantic</p>
          <h1>Find the evidence, not the noise.</h1>
          <p>
            Search package names, versions, breaking changes, advisories, and underlying concepts.
          </p>
        </div>
        <div className="search-index-status">
          <span>PostgreSQL hybrid</span>
          <strong>RRF</strong>
          <small>keyword + vector + typo tolerance</small>
        </div>
      </header>

      <search>
        <form action="/search" className="search-form" method="get">
          <div className="search-query-row">
            <label>
              <span>Search intelligence</span>
              <input
                defaultValue={requested.q}
                minLength={2}
                name="q"
                placeholder="e.g. Go 1.27 range-over-func breaking changes"
                required
                type="search"
              />
            </label>
            <button type="submit">Search corpus</button>
            <Link href={"/search" as Route}>Clear</Link>
          </div>
          <details className="search-filters" open={Object.keys(requested).length > 1}>
            <summary>Exact filters</summary>
            <div>
              <label>
                Topic or package
                <input defaultValue={requested.topic} name="topic" placeholder="Go" />
              </label>
              <label>
                Source tier
                <select defaultValue={requested.sourceTier ?? ""} name="sourceTier">
                  <option value="">Any tier</option>
                  <option value="T0">T0 · official</option>
                  <option value="T1">T1 · maintainer</option>
                  <option value="T2">T2 · expert context</option>
                  <option value="T3">T3 · community</option>
                </select>
              </label>
              <label>
                Lifecycle
                <select defaultValue={requested.lifecycle ?? ""} name="lifecycle">
                  <option value="">Any state</option>
                  <option value="stable">Stable</option>
                  <option value="preview">Preview</option>
                  <option value="release_candidate">Release candidate</option>
                  <option value="deprecated">Deprecated</option>
                  <option value="eol">End of life</option>
                </select>
              </label>
              <label>
                Recommended action
                <input defaultValue={requested.action} name="action" placeholder="Upgrade" />
              </label>
              <label>
                After
                <input defaultValue={requested.after} name="after" type="date" />
              </label>
              <label>
                Before
                <input defaultValue={requested.before} name="before" type="date" />
              </label>
              <label>
                Radar state
                <select defaultValue={requested.radar ?? ""} name="radar">
                  <option value="">Any decision</option>
                  <option value="adopt">Adopt</option>
                  <option value="trial">Trial</option>
                  <option value="assess">Assess</option>
                  <option value="hold">Hold</option>
                  <option value="reject">Reject</option>
                </select>
              </label>
              <label>
                Saved stories
                <select defaultValue={requested.saved ?? ""} name="saved">
                  <option value="">Saved or unsaved</option>
                  <option value="true">Saved only</option>
                  <option value="false">Unsaved only</option>
                </select>
              </label>
            </div>
          </details>
        </form>
      </search>

      <SavedSearches
        currentFilters={currentFilters}
        currentQuery={parameters.q}
        initialSearches={savedSearches}
      />

      {parameters.q.length < 2 ? (
        <section className="search-start-state">
          <p className="eyebrow">Search explanation</p>
          <h2>One query, three retrieval paths.</h2>
          <p>
            Exact package and title matches, full-text ranking, and model-versioned semantic
            similarity are fused into one inspectable result list.
          </p>
        </section>
      ) : response === null ? (
        <div className="reading-empty-state">
          <p className="eyebrow">Read path degraded</p>
          <h2>Search is temporarily unavailable.</h2>
          <p>The private API failed closed. Your query remains in this page URL for retry.</p>
        </div>
      ) : (
        <SearchResults response={response} timezone={owner.timezone} />
      )}
    </>
  );
};

export default SearchPage;
