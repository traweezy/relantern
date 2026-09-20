import { getOwnerSession } from "@/server/auth/session";
import { getLiveSnapshot } from "@/server/intelligence/client";

export const GET = async (): Promise<Response> => {
  if ((await getOwnerSession()) === null) {
    return Response.json(
      {
        detail: "An authenticated owner session is required.",
        status: 401,
        title: "Unauthorized",
        type: "about:blank",
      },
      { status: 401, headers: { "Cache-Control": "no-store" } },
    );
  }
  try {
    return Response.json(await getLiveSnapshot(), { headers: { "Cache-Control": "no-store" } });
  } catch {
    return Response.json(
      {
        detail: "The private live snapshot is unavailable.",
        status: 503,
        title: "Service Unavailable",
        type: "about:blank",
      },
      { status: 503, headers: { "Cache-Control": "no-store" } },
    );
  }
};
