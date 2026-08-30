import { opmlCommitSchema } from "@/features/discovery/commands";
import { commitOPML } from "@/server/discovery/client";
import { ownerDiscoveryJSONRoute, readDiscoveryJSON } from "@/server/discovery/route";

export const POST = async (request: Request): Promise<Response> =>
  ownerDiscoveryJSONRoute(async (owner) =>
    commitOPML(owner.userID, await readDiscoveryJSON(request, opmlCommitSchema)),
  );
