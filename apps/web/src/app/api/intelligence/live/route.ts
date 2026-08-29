import { getOwnerSession } from "@/server/auth/session";
import { getLiveSnapshot } from "@/server/intelligence/client";

const encoder = new TextEncoder();

export const GET = async (request: Request): Promise<Response> => {
  if ((await getOwnerSession()) === null) {
    return Response.json(
      {
        detail: "An authenticated owner session is required.",
        status: 401,
        title: "Unauthorized",
        type: "about:blank",
      },
      { status: 401 },
    );
  }

  let timer: ReturnType<typeof setTimeout> | undefined;
  let closed = false;
  const stream = new ReadableStream<Uint8Array>({
    cancel() {
      closed = true;
      if (timer !== undefined) {
        clearTimeout(timer);
      }
    },
    start(controller) {
      const publish = async () => {
        if (closed || request.signal.aborted) {
          controller.close();
          return;
        }
        try {
          const snapshot = await getLiveSnapshot();
          if (closed || request.signal.aborted) {
            return;
          }
          const payload = `event: snapshot\ndata: ${JSON.stringify(snapshot)}\n\n`;
          controller.enqueue(encoder.encode(payload));
        } catch {
          if (!closed && !request.signal.aborted) {
            controller.enqueue(encoder.encode(": private read retry\n\n"));
          }
        }
        if (!closed && !request.signal.aborted) {
          timer = setTimeout(publish, 1_000);
        }
      };
      request.signal.addEventListener(
        "abort",
        () => {
          closed = true;
          if (timer !== undefined) {
            clearTimeout(timer);
          }
        },
        { once: true },
      );
      void publish();
    },
  });

  return new Response(stream, {
    headers: {
      "Cache-Control": "no-cache, no-store, private",
      Connection: "keep-alive",
      "Content-Type": "text/event-stream; charset=utf-8",
      "X-Accel-Buffering": "no",
      "X-Content-Type-Options": "nosniff",
    },
  });
};
