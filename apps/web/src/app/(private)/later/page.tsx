import {
  collectionMetadata,
  ReadingCollectionPage,
} from "@/features/reading-state/collection-page";

type PageProps = Readonly<{ searchParams: Promise<Readonly<{ cursor?: string }>> }>;

export const metadata = collectionMetadata("Read Later");

const LaterPage = async ({ searchParams }: PageProps) => (
  <ReadingCollectionPage
    cursor={(await searchParams).cursor}
    description="An intentional reading queue that stays independent from stars and read state."
    eyebrow="Active queue"
    kind="later"
    title="Read Later"
  />
);

export default LaterPage;
