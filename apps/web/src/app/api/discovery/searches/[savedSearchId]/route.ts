import { z } from "zod";
import { deleteSavedSearch } from "@/server/discovery/client";
import { DiscoveryRequestError, ownerDiscoveryJSONRoute } from "@/server/discovery/route";

type RouteContext = Readonly<{ params: Promise<Readonly<{ savedSearchId: string }>> }>;

export const DELETE = async (_request: Request, context: RouteContext): Promise<Response> =>
  ownerDiscoveryJSONRoute(async (owner) => {
    const parsed = z.uuid().safeParse((await context.params).savedSearchId);
    if (!parsed.success) {
      throw new DiscoveryRequestError("The saved search ID is invalid.");
    }
    await deleteSavedSearch(owner.userID, parsed.data);
    return { deleted: true };
  });
