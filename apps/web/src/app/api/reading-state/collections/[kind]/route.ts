import { z } from "zod";
import { getReadingCollection } from "@/server/reading-state/client";
import { ownerJSONRoute, ReadingStateRequestError } from "@/server/reading-state/route";

type CollectionRouteContext = Readonly<{
  params: Promise<Readonly<{ kind: string }>>;
}>;

const collectionKindSchema = z.enum(["archive", "inbox", "later", "snoozed", "starred"]);

export const GET = async (request: Request, context: CollectionRouteContext): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const parsedKind = collectionKindSchema.safeParse((await context.params).kind);
    if (!parsedKind.success) {
      throw new ReadingStateRequestError("The requested collection does not exist.");
    }
    const cursor = new URL(request.url).searchParams.get("cursor") ?? undefined;
    if (cursor !== undefined && cursor.length > 512) {
      throw new ReadingStateRequestError("The collection cursor is invalid.");
    }
    return getReadingCollection(parsedKind.data, owner.userID, cursor);
  });
