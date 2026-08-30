import { radarDecisionCommandSchema } from "@/features/control-plane/commands";
import { recordRadarDecision } from "@/server/controlplane/client";
import { ownerControlPlaneJSONRoute, readControlPlaneJSON } from "@/server/controlplane/route";

type RouteContext = Readonly<{ params: Promise<{ candidateId: string }> }>;

export const POST = async (request: Request, context: RouteContext): Promise<Response> => {
  const [command, parameters] = await Promise.all([
    readControlPlaneJSON(request, radarDecisionCommandSchema),
    context.params,
  ]);
  return ownerControlPlaneJSONRoute((owner) =>
    recordRadarDecision(owner.userID, parameters.candidateId, command),
  );
};
