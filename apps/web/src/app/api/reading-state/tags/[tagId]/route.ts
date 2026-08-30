import { z } from "zod";
import { tagInputSchema } from "@/features/reading-state/commands";
import { deleteTag, updateTag } from "@/server/reading-state/client";
import {
  ownerJSONRoute,
  ReadingStateRequestError,
  readValidatedJSON,
} from "@/server/reading-state/route";

type TagRouteContext = Readonly<{
  params: Promise<Readonly<{ tagId: string }>>;
}>;

const tagIDSchema = z.uuid();

const tagIDFromContext = async (context: TagRouteContext): Promise<string> => {
  const tagID = tagIDSchema.safeParse((await context.params).tagId);
  if (!tagID.success) {
    throw new ReadingStateRequestError("The tag ID is invalid.");
  }
  return tagID.data;
};

export const PATCH = async (request: Request, context: TagRouteContext): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const [tagID, input] = await Promise.all([
      tagIDFromContext(context),
      readValidatedJSON(request, tagInputSchema),
    ]);
    return updateTag(owner.userID, tagID, input);
  });

export const DELETE = async (_request: Request, context: TagRouteContext): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    await deleteTag(owner.userID, await tagIDFromContext(context));
    return { deleted: true };
  });
