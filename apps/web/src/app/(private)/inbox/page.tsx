import {
  collectionMetadata,
  ReadingCollectionPage,
} from "@/features/reading-state/collection-page";

type PageProps = Readonly<{ searchParams: Promise<Readonly<{ cursor?: string }>> }>;

export const metadata = collectionMetadata("Inbox");

const InboxPage = async ({ searchParams }: PageProps) => (
  <ReadingCollectionPage
    cursor={(await searchParams).cursor}
    description="Published, unsnoozed evidence waiting for a deliberate triage decision."
    eyebrow="Active queue"
    kind="inbox"
    title="Inbox"
  />
);

export default InboxPage;
