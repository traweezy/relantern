import { saveSearchSchema } from "@/features/discovery/commands";
import { getSavedSearches, saveSearch } from "@/server/discovery/client";
import { ownerDiscoveryJSONRoute, readDiscoveryJSON } from "@/server/discovery/route";

export const GET = async (): Promise<Response> =>
  ownerDiscoveryJSONRoute(async (owner) => ({
    searches: await getSavedSearches(owner.userID),
  }));

export const POST = async (request: Request): Promise<Response> =>
  ownerDiscoveryJSONRoute(async (owner) =>
    saveSearch(owner.userID, await readDiscoveryJSON(request, saveSearchSchema)),
  );
