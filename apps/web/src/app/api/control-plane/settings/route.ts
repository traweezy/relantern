import { settingsCommandSchema } from "@/features/control-plane/commands";
import { updateSettings } from "@/server/controlplane/client";
import { ownerControlPlaneJSONRoute, readControlPlaneJSON } from "@/server/controlplane/route";

export const PUT = async (request: Request): Promise<Response> => {
  const command = await readControlPlaneJSON(request, settingsCommandSchema);
  return ownerControlPlaneJSONRoute((owner) => updateSettings(owner.userID, command));
};
