import { laterOrderSchema } from "@/features/reading-state/commands";
import { reorderLater } from "@/server/reading-state/client";
import { ownerJSONRoute, readValidatedJSON } from "@/server/reading-state/route";

export const PUT = async (request: Request): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const command = await readValidatedJSON(request, laterOrderSchema);
    return reorderLater(owner.userID, command);
  });
