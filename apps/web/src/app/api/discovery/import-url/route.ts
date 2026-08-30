import { manualCaptureSchema } from "@/features/discovery/commands";
import { importInboxURL } from "@/server/discovery/client";
import { ownerDiscoveryJSONRoute, readDiscoveryJSON } from "@/server/discovery/route";

export const POST = async (request: Request): Promise<Response> =>
  ownerDiscoveryJSONRoute(async (owner) =>
    importInboxURL(owner.userID, await readDiscoveryJSON(request, manualCaptureSchema)),
  );
