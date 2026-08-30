import { ownerControlPlaneJSONRoute } from "@/server/controlplane/route";
import { retryDigestDelivery } from "@/server/digest/client";

type Context = Readonly<{ params: Promise<{ digestId: string }> }>;

export const POST = async (_request: Request, context: Context): Promise<Response> => {
  const { digestId } = await context.params;
  return ownerControlPlaneJSONRoute((owner) => retryDigestDelivery(owner.userID, digestId));
};
