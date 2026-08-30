import {
  collectionMetadata,
  ReadingCollectionPage,
} from "@/features/reading-state/collection-page";

type PageProps = Readonly<{ searchParams: Promise<Readonly<{ cursor?: string }>> }>;

export const metadata = collectionMetadata("Snoozed");

const SnoozedPage = async ({ searchParams }: PageProps) => (
  <ReadingCollectionPage
    cursor={(await searchParams).cursor}
    description="Stories hidden until their explicit return time, with their prior queue retained."
    eyebrow="Scheduled return"
    kind="snoozed"
    title="Snoozed"
  />
);

export default SnoozedPage;
