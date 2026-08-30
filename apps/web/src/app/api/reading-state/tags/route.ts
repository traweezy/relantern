import { tagInputSchema } from "@/features/reading-state/commands";
import { createTag, getTags } from "@/server/reading-state/client";
import { ownerJSONRoute, readValidatedJSON } from "@/server/reading-state/route";

export const GET = async (): Promise<Response> =>
  ownerJSONRoute(async (owner) => ({ tags: await getTags(owner.userID) }));

export const POST = async (request: Request): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const input = await readValidatedJSON(request, tagInputSchema);
    return createTag(owner.userID, input);
  });
