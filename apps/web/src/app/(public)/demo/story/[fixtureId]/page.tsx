import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { connection } from "next/server";
import { getDemoStory } from "@/features/demo/demo-adapter";
import { DemoItemControls } from "@/features/demo/demo-item-controls";
import { StoryDetail } from "@/features/intelligence/story-detail";

type DemoStoryPageProps = Readonly<{
  params: Promise<Readonly<{ fixtureId: string }>>;
}>;

export const metadata: Metadata = {
  robots: { follow: false, index: false },
  title: "Illustrative story | Relantern demonstration",
};

export const instant = false;

const DemoStoryPage = async ({ params }: DemoStoryPageProps) => {
  await connection();
  const { fixtureId } = await params;
  const story = getDemoStory(fixtureId);
  if (story === null) {
    notFound();
  }
  return (
    <div className="demo-story-page">
      <DemoItemControls storyID={story.id} />
      <StoryDetail relatedHrefPrefix="/demo/story" story={story} />
    </div>
  );
};

export default DemoStoryPage;
