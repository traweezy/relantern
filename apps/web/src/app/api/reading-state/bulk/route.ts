import { bulkReadingMutationSchema } from "@/features/reading-state/commands";
import { bulkMutateStoryState } from "@/server/reading-state/client";
import { ownerJSONRoute, readValidatedJSON } from "@/server/reading-state/route";

export const POST = async (request: Request): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const command = await readValidatedJSON(request, bulkReadingMutationSchema);
    return bulkMutateStoryState(owner.userID, command);
  });
