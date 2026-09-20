import { notFound } from "next/navigation";
import { getDemoStory } from "@/features/demo/demo-adapter";
import { DemoItemControls } from "@/features/demo/demo-item-controls";
import { demoFixtureIDs } from "@/features/demo/demo-route";
import { StoryDetail } from "@/features/intelligence/story-detail";

export const generateStaticParams = () => demoFixtureIDs.map((fixtureId) => ({ fixtureId }));
export const dynamicParams = false;

const DemoStoryPage = async ({
  params,
}: Readonly<{ params: Promise<Readonly<{ fixtureId: string }>> }>) => {
  const { fixtureId } = await params;
  const story = getDemoStory(fixtureId);
  if (story === null) notFound();

  return (
    <div className="demo-story-page">
      <DemoItemControls storyID={story.id} />
      <StoryDetail relatedHrefPrefix="/demo/story" showAnnotationHelp={false} story={story} />
    </div>
  );
};

export default DemoStoryPage;
