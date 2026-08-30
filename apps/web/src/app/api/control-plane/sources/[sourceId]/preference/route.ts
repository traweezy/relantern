import { sourcePreferenceCommandSchema } from "@/features/control-plane/commands";
import { updateSourcePreference } from "@/server/controlplane/client";
import { ownerControlPlaneJSONRoute, readControlPlaneJSON } from "@/server/controlplane/route";

type RouteContext = Readonly<{ params: Promise<{ sourceId: string }> }>;

export const PUT = async (request: Request, context: RouteContext): Promise<Response> => {
  const [command, parameters] = await Promise.all([
    readControlPlaneJSON(request, sourcePreferenceCommandSchema),
    context.params,
  ]);
  return ownerControlPlaneJSONRoute((owner) =>
    updateSourcePreference(owner.userID, parameters.sourceId, command),
  );
};
