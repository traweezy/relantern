import { opmlPreviewSchema } from "@/features/discovery/commands";
import { previewOPML } from "@/server/discovery/client";
import { ownerDiscoveryJSONRoute, readDiscoveryJSON } from "@/server/discovery/route";

export const POST = async (request: Request): Promise<Response> =>
  ownerDiscoveryJSONRoute(async (owner) =>
    previewOPML(owner.userID, await readDiscoveryJSON(request, opmlPreviewSchema)),
  );
