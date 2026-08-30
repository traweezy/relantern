import {
  collectionMetadata,
  ReadingCollectionPage,
} from "@/features/reading-state/collection-page";

type PageProps = Readonly<{ searchParams: Promise<Readonly<{ cursor?: string }>> }>;

export const metadata = collectionMetadata("Starred");

const StarredPage = async ({ searchParams }: PageProps) => (
  <ReadingCollectionPage
    cursor={(await searchParams).cursor}
    description="Durable high-value references from Inbox, Later, and Archive."
    eyebrow="Knowledge shelf"
    kind="starred"
    title="Starred"
  />
);

export default StarredPage;
