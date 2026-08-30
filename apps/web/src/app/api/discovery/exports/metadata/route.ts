import { z } from "zod";
import { exportDiscoveryFile } from "@/server/discovery/client";
import { DiscoveryRequestError, ownerDiscoveryFileRoute } from "@/server/discovery/route";

const formatSchema = z.enum(["csv", "json"]);

export const GET = async (request: Request): Promise<Response> =>
  ownerDiscoveryFileRoute((owner) => {
    const parsed = formatSchema.safeParse(
      new URL(request.url).searchParams.get("format") ?? "json",
    );
    if (!parsed.success) {
      throw new DiscoveryRequestError("Metadata format must be JSON or CSV.");
    }
    return exportDiscoveryFile(
      owner.userID,
      `/api/v1/exports/metadata?format=${encodeURIComponent(parsed.data)}`,
    );
  });
