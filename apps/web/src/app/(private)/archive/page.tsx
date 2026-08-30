import {
  collectionMetadata,
  ReadingCollectionPage,
} from "@/features/reading-state/collection-page";

type PageProps = Readonly<{ searchParams: Promise<Readonly<{ cursor?: string }>> }>;

export const metadata = collectionMetadata("Archive");

const ArchivePage = async ({ searchParams }: PageProps) => (
  <ReadingCollectionPage
    cursor={(await searchParams).cursor}
    description="Searchable history with stars, tags, notes, highlights, and progress preserved."
    eyebrow="Retained history"
    kind="archive"
    title="Archive"
  />
);

export default ArchivePage;
