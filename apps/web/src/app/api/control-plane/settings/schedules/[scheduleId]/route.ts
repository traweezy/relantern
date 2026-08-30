import { scheduleCommandSchema } from "@/features/control-plane/commands";
import { updateSchedule } from "@/server/controlplane/client";
import { ownerControlPlaneJSONRoute, readControlPlaneJSON } from "@/server/controlplane/route";

type RouteContext = Readonly<{ params: Promise<{ scheduleId: string }> }>;

export const PUT = async (request: Request, context: RouteContext): Promise<Response> => {
  const [command, parameters] = await Promise.all([
    readControlPlaneJSON(request, scheduleCommandSchema),
    context.params,
  ]);
  return ownerControlPlaneJSONRoute((owner) =>
    updateSchedule(owner.userID, parameters.scheduleId, command),
  );
};
