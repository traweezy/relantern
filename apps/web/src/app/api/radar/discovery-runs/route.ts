import { radarDiscoveryCommandSchema } from "@/features/control-plane/commands";
import { queueRadarDiscovery } from "@/server/controlplane/client";
import { ownerControlPlaneJSONRoute, readControlPlaneJSON } from "@/server/controlplane/route";

export const POST = async (request: Request): Promise<Response> => {
  const command = await readControlPlaneJSON(request, radarDiscoveryCommandSchema);
  return ownerControlPlaneJSONRoute((owner) => queueRadarDiscovery(owner.userID, command));
};
