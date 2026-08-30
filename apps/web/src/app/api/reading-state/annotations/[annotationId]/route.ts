import { z } from "zod";
import { annotationUpdateSchema } from "@/features/reading-state/commands";
import { deleteAnnotation, updateAnnotation } from "@/server/reading-state/client";
import {
  ownerJSONRoute,
  ReadingStateRequestError,
  readValidatedJSON,
} from "@/server/reading-state/route";

type AnnotationRouteContext = Readonly<{
  params: Promise<Readonly<{ annotationId: string }>>;
}>;

const annotationIDSchema = z.uuid();

const annotationIDFromContext = async (context: AnnotationRouteContext): Promise<string> => {
  const annotationID = annotationIDSchema.safeParse((await context.params).annotationId);
  if (!annotationID.success) {
    throw new ReadingStateRequestError("The annotation ID is invalid.");
  }
  return annotationID.data;
};

export const PATCH = async (request: Request, context: AnnotationRouteContext): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    const [annotationID, input] = await Promise.all([
      annotationIDFromContext(context),
      readValidatedJSON(request, annotationUpdateSchema),
    ]);
    return updateAnnotation(owner.userID, annotationID, input);
  });

export const DELETE = async (
  _request: Request,
  context: AnnotationRouteContext,
): Promise<Response> =>
  ownerJSONRoute(async (owner) => {
    await deleteAnnotation(owner.userID, await annotationIDFromContext(context));
    return { deleted: true };
  });
