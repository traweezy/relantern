import { bulkUndoSchema } from "@/features/reading-state/commands";
import { undoBulkStoryState } from "@/server/reading-state/client";
import { ownerJSONRoute, readValidatedJSON } from "@/server/reading-state/route";

export const POST = async (request: Request): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const command = await readValidatedJSON(request, bulkUndoSchema);
    return undoBulkStoryState(owner.userID, command.bulkId);
  });
