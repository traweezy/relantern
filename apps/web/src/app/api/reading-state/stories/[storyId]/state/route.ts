import { z } from "zod";
import { readingMutationSchema } from "@/features/reading-state/commands";
import { getStoryState, mutateStoryState } from "@/server/reading-state/client";
import {
  ownerJSONRoute,
  ReadingStateRequestError,
  readValidatedJSON,
} from "@/server/reading-state/route";

type StoryRouteContext = Readonly<{
  params: Promise<Readonly<{ storyId: string }>>;
}>;

const storyIDSchema = z.uuid();

const storyIDFromContext = async (context: StoryRouteContext): Promise<string> => {
  const storyID = storyIDSchema.safeParse((await context.params).storyId);
  if (!storyID.success) {
    throw new ReadingStateRequestError("The story ID is invalid.");
  }
  return storyID.data;
};

export const GET = async (_request: Request, context: StoryRouteContext): Promise<Response> =>
  ownerJSONRoute(async (owner) => getStoryState(owner.userID, await storyIDFromContext(context)));

export const PATCH = async (request: Request, context: StoryRouteContext): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const [storyID, command] = await Promise.all([
      storyIDFromContext(context),
      readValidatedJSON(request, readingMutationSchema),
    ]);
    return mutateStoryState(owner.userID, storyID, command);
  });
