import { markdownExportSchema } from "@/features/discovery/commands";
import { exportDiscoveryFile } from "@/server/discovery/client";
import { ownerDiscoveryFileRoute, readDiscoveryJSON } from "@/server/discovery/route";

export const POST = async (request: Request): Promise<Response> =>
  ownerDiscoveryFileRoute(async (owner) =>
    exportDiscoveryFile(owner.userID, "/api/v1/exports/markdown", {
      body: await readDiscoveryJSON(request, markdownExportSchema),
      method: "POST",
    }),
  );
