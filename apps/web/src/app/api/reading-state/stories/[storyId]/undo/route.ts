import { z } from "zod";
import { undoSchema } from "@/features/reading-state/commands";
import { undoStoryState } from "@/server/reading-state/client";
import {
  ownerJSONRoute,
  ReadingStateRequestError,
  readValidatedJSON,
} from "@/server/reading-state/route";

type StoryRouteContext = Readonly<{
  params: Promise<Readonly<{ storyId: string }>>;
}>;

const storyIDSchema = z.uuid();

export const POST = async (request: Request, context: StoryRouteContext): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const storyID = storyIDSchema.safeParse((await context.params).storyId);
    if (!storyID.success) {
      throw new ReadingStateRequestError("The story ID is invalid.");
    }
    const command = await readValidatedJSON(request, undoSchema);
    return undoStoryState(owner.userID, storyID.data, command.mutationId);
  });
