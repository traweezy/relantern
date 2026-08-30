import type { Metadata } from "next";
import { TriageCollection } from "@/features/reading-state/triage-collection";
import { requireOwnerSession } from "@/server/auth/session";
import { getReadingCollection, getTags } from "@/server/reading-state/client";

type CollectionKind = "archive" | "inbox" | "later" | "snoozed" | "starred";

type ReadingCollectionPageProps = Readonly<{
  cursor: string | undefined;
  description: string;
  eyebrow: string;
  kind: CollectionKind;
  title: string;
}>;

export const collectionMetadata = (title: string): Metadata => ({
  title: `${title} | Relantern`,
});

export const ReadingCollectionPage = async ({
  cursor,
  description,
  eyebrow,
  kind,
  title,
}: ReadingCollectionPageProps) => {
  const owner = await requireOwnerSession();
  const [collection, tags] = await Promise.all([
    getReadingCollection(kind, owner.userID, cursor),
    getTags(owner.userID),
  ]);
  return (
    <>
      <header className="intelligence-header reading-collection-header">
        <div>
          <p className="eyebrow">{eyebrow}</p>
          <h1>{title}</h1>
          <p>{description}</p>
        </div>
        <div className="collection-count-block">
          <strong>{collection.total}</strong>
          <span>stories</span>
        </div>
      </header>
      <TriageCollection
        initialCollection={collection}
        kind={kind}
        tags={tags}
        timezone={owner.timezone}
      />
    </>
  );
};
