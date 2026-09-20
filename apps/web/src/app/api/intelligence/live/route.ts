import { getOwnerSession } from "@/server/auth/session";
import { getIntelligenceAPIConfiguration } from "@/server/intelligence/config";

const cursorPattern = /^(0|[1-9][0-9]{0,18})$/;
const maximumStreamMilliseconds = 5 * 60 * 1_000;

const problem = (status: number, detail: string): Response =>
  Response.json(
    {
      detail,
      status,
      title: status === 401 ? "Unauthorized" : "Service Unavailable",
      type: "about:blank",
    },
    { status, headers: { "Cache-Control": "no-store" } },
  );

export const GET = async (request: Request): Promise<Response> => {
  const owner = await getOwnerSession();
  if (owner === null) {
    return problem(401, "An authenticated owner session is required.");
  }

  const requestedCursor =
    new URL(request.url).searchParams.get("after") ?? request.headers.get("Last-Event-ID");
  if (requestedCursor !== null && !cursorPattern.test(requestedCursor)) {
    return Response.json(
      {
        detail: "The live event cursor is invalid.",
        status: 400,
        title: "Bad Request",
        type: "about:blank",
      },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }

  let configuration: ReturnType<typeof getIntelligenceAPIConfiguration>;
  let target: URL;
  try {
    configuration = getIntelligenceAPIConfiguration();
    target = new URL("/internal/v1/live/stream", configuration.baseURL);
  } catch {
    return problem(503, "The private live stream is unavailable.");
  }
  if (requestedCursor !== null) {
    target.searchParams.set("after", requestedCursor);
  }
  const abortController = new AbortController();
  const remainingSessionMilliseconds = Math.max(0, owner.expiresAt.getTime() - Date.now());
  const timeout = setTimeout(
    () => abortController.abort(),
    Math.min(maximumStreamMilliseconds, remainingSessionMilliseconds),
  );
  const onAbort = () => abortController.abort();
  const cleanup = () => {
    clearTimeout(timeout);
    request.signal.removeEventListener("abort", onAbort);
  };
  request.signal.addEventListener("abort", onAbort, { once: true });
  let upstream: Response;
  try {
    upstream = await fetch(target, {
      cache: "no-store",
      headers: {
        accept: "text/event-stream",
        authorization: `Bearer ${configuration.serviceToken}`,
      },
      signal: abortController.signal,
    });
  } catch {
    cleanup();
    return problem(503, "The private live stream is unavailable.");
  }
  if (
    !upstream.ok ||
    !upstream.body ||
    !(upstream.headers.get("content-type") ?? "").startsWith("text/event-stream")
  ) {
    cleanup();
    try {
      await upstream.body?.cancel();
    } catch {
      // The upstream already failed; preserve the stable problem response.
    }
    return problem(503, "The private live stream is unavailable.");
  }
  const reader = upstream.body.getReader();
  const body = new ReadableStream<Uint8Array>({
    async pull(controller) {
      try {
        const chunk = await reader.read();
        if (chunk.done) {
          cleanup();
          controller.close();
        } else {
          controller.enqueue(chunk.value);
        }
      } catch (error: unknown) {
        cleanup();
        controller.error(error);
      }
    },
    async cancel(reason: unknown) {
      cleanup();
      abortController.abort();
      await reader.cancel(reason);
    },
  });
  return new Response(body, {
    headers: {
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "Content-Type": "text/event-stream; charset=utf-8",
      "X-Accel-Buffering": "no",
      "X-Content-Type-Options": "nosniff",
    },
  });
};
