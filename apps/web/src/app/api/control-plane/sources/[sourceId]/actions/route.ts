import { sourceActionCommandSchema } from "@/features/control-plane/commands";
import { actOnSource } from "@/server/controlplane/client";
import { ownerControlPlaneJSONRoute, readControlPlaneJSON } from "@/server/controlplane/route";

type RouteContext = Readonly<{ params: Promise<{ sourceId: string }> }>;

export const POST = async (request: Request, context: RouteContext): Promise<Response> => {
  const [command, parameters] = await Promise.all([
    readControlPlaneJSON(request, sourceActionCommandSchema),
    context.params,
  ]);
  return ownerControlPlaneJSONRoute((owner) =>
    actOnSource(owner.userID, parameters.sourceId, command),
  );
};
