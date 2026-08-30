import { z } from "zod";
import { feedbackCommandSchema } from "@/features/reading-state/commands";
import { recordStoryFeedback } from "@/server/reading-state/client";
import {
  ownerJSONRoute,
  ReadingStateRequestError,
  readValidatedJSON,
} from "@/server/reading-state/route";

type FeedbackRouteContext = Readonly<{
  params: Promise<Readonly<{ storyId: string }>>;
}>;

const storyIDSchema = z.uuid();

export const POST = async (request: Request, context: FeedbackRouteContext): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const [storyIDResult, command] = await Promise.all([
      storyIDSchema.safeParseAsync((await context.params).storyId),
      readValidatedJSON(request, feedbackCommandSchema),
    ]);
    if (!storyIDResult.success) {
      throw new ReadingStateRequestError("The story ID is invalid.");
    }
    return recordStoryFeedback(owner.userID, storyIDResult.data, command);
  });
