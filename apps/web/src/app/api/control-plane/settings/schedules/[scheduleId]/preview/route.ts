import { previewSchedule } from "@/server/controlplane/client";
import { ownerControlPlaneJSONRoute } from "@/server/controlplane/route";

type RouteContext = Readonly<{ params: Promise<{ scheduleId: string }> }>;

export const GET = async (_request: Request, context: RouteContext): Promise<Response> => {
  const parameters = await context.params;
  return ownerControlPlaneJSONRoute((owner) =>
    previewSchedule(owner.userID, parameters.scheduleId),
  );
};
