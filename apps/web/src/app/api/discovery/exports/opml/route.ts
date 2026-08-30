import { exportDiscoveryFile } from "@/server/discovery/client";
import { ownerDiscoveryFileRoute } from "@/server/discovery/route";

export const GET = async (): Promise<Response> =>
  ownerDiscoveryFileRoute((owner) => exportDiscoveryFile(owner.userID, "/api/v1/exports/opml"));
