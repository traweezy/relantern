import { z } from "zod";
import { annotationInputSchema } from "@/features/reading-state/commands";
import { createAnnotation, getAnnotations } from "@/server/reading-state/client";
import {
  ownerJSONRoute,
  ReadingStateRequestError,
  readValidatedJSON,
} from "@/server/reading-state/route";

type AnnotationRouteContext = Readonly<{
  params: Promise<Readonly<{ storyId: string }>>;
}>;

const storyIDSchema = z.uuid();

const storyIDFromContext = async (context: AnnotationRouteContext): Promise<string> => {
  const storyID = storyIDSchema.safeParse((await context.params).storyId);
  if (!storyID.success) {
    throw new ReadingStateRequestError("The story ID is invalid.");
  }
  return storyID.data;
};

export const GET = async (_request: Request, context: AnnotationRouteContext): Promise<Response> =>
  ownerJSONRoute(async (owner) => ({
    annotations: await getAnnotations(owner.userID, await storyIDFromContext(context)),
  }));

export const POST = async (request: Request, context: AnnotationRouteContext): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const [storyID, input] = await Promise.all([
      storyIDFromContext(context),
      readValidatedJSON(request, annotationInputSchema),
    ]);
    return createAnnotation(owner.userID, storyID, input);
  });
