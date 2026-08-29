import type { StoryDetail as StoryDetailModel } from "@relantern/domain";
import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { connection } from "next/server";
import { StoryDetail } from "@/features/intelligence/story-detail";
import { requireOwnerSession } from "@/server/auth/session";
import { getStory, IntelligenceResponseError } from "@/server/intelligence/client";

type StoryPageProps = Readonly<{
  params: Promise<Readonly<{ storyId: string }>>;
}>;

const storyIDPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export const metadata: Metadata = {
  title: "Story | Relantern",
};

export const instant = false;

const StoryPage = async ({ params }: StoryPageProps) => {
  await connection();
  const owner = await requireOwnerSession();
  const { storyId } = await params;
  if (!storyIDPattern.test(storyId)) {
    notFound();
  }
  let story: StoryDetailModel;
  try {
    story = await getStory(storyId);
  } catch (error: unknown) {
    if (error instanceof IntelligenceResponseError && error.status === 404) {
      notFound();
    }
    throw error;
  }
  return <StoryDetail story={story} timezone={owner.timezone} />;
};

export default StoryPage;
