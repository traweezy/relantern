import type { Metadata } from "next";
import { ReleaseCatalog } from "@/features/discovery/release-catalog";
import { requireOwnerSession } from "@/server/auth/session";
import { getReleaseCatalog } from "@/server/discovery/client";

export const metadata: Metadata = { title: "Releases | Relantern" };

const ReleasesPage = async () => {
  const owner = await requireOwnerSession();
  const catalog = await getReleaseCatalog(owner.userID).catch(() => null);

  return (
    <>
      <header className="intelligence-header releases-header">
        <div>
          <p className="eyebrow">Release intelligence · source verified</p>
          <h1>Know what moved—and what moves next.</h1>
          <p>
            Watched versions, newest evidence-backed releases, and declared timelines without
            invented dates.
          </p>
        </div>
        {catalog !== null && (
          <div className="release-generated">
            <span>Catalog verified</span>
            <strong>
              {new Intl.DateTimeFormat("en-US", {
                dateStyle: "medium",
                timeStyle: "short",
                timeZone: owner.timezone,
              }).format(new Date(catalog.generatedAt))}
            </strong>
          </div>
        )}
      </header>
      {catalog === null ? (
        <div className="reading-empty-state">
          <p className="eyebrow">Read path degraded</p>
          <h2>Release intelligence is temporarily unavailable.</h2>
          <p>The private API failed closed. No unverified fallback data is displayed.</p>
        </div>
      ) : (
        <ReleaseCatalog catalog={catalog} timezone={owner.timezone} />
      )}
    </>
  );
};

export default ReleasesPage;
