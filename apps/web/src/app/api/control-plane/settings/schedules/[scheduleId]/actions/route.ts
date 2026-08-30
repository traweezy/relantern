import { scheduleActionCommandSchema } from "@/features/control-plane/commands";
import { actOnSchedule } from "@/server/controlplane/client";
import { ownerControlPlaneJSONRoute, readControlPlaneJSON } from "@/server/controlplane/route";

type RouteContext = Readonly<{ params: Promise<{ scheduleId: string }> }>;

export const POST = async (request: Request, context: RouteContext): Promise<Response> => {
  const [command, parameters] = await Promise.all([
    readControlPlaneJSON(request, scheduleActionCommandSchema),
    context.params,
  ]);
  return ownerControlPlaneJSONRoute((owner) =>
    actOnSchedule(owner.userID, parameters.scheduleId, command),
  );
};
