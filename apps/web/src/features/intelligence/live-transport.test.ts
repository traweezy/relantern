import { afterEach, describe, expect, it, vi } from "vitest";
import { demoSnapshot } from "../demo/demo-snapshot";
import { type LiveTransportError, liveTransport } from "./live-transport";

const streamResponse = (chunks: readonly string[]): Response => {
  const encoder = new TextEncoder();
  return new Response(
    new ReadableStream({
      start(controller) {
        for (const chunk of chunks) {
          controller.enqueue(encoder.encode(chunk));
        }
        controller.close();
      },
    }),
    { headers: { "content-type": "text/event-stream; charset=utf-8" } },
  );
};

afterEach(() => vi.unstubAllGlobals());

describe("live replay transport", () => {
  it("parses split SSE events and advances by the durable ID", async () => {
    const story = demoSnapshot.live.events[0]?.story;
    expect(story).toBeDefined();
    const event = {
      id: "42",
      observedAt: "2026-09-19T12:00:00Z",
      story,
      type: "story-created",
    };
    const encoded = `id: 42\nevent: story-created\ndata: ${JSON.stringify(event)}\n\n`;
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(
        streamResponse([encoded.slice(0, 19), encoded.slice(19, 91), encoded.slice(91)]),
      );
    vi.stubGlobal("fetch", fetchMock);
    const received: string[] = [];
    const connected = vi.fn();

    const outcome = await liveTransport.read(
      "41",
      new AbortController().signal,
      (value) => received.push(value.id),
      connected,
    );

    expect(outcome).toBe("ended");
    expect(received).toEqual(["42"]);
    expect(connected).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/intelligence/live?after=41");
  });

  it("requests a fresh snapshot when the replay cursor expires", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(
          streamResponse(['event: reset_required\ndata: {"reason":"cursor_expired"}\n\n']),
        ),
    );
    expect(await liveTransport.read("12", new AbortController().signal, vi.fn(), vi.fn())).toBe(
      "reset",
    );
  });

  it("surfaces authentication status before reading the body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 401 })),
    );
    await expect(
      liveTransport.read("0", new AbortController().signal, vi.fn(), vi.fn()),
    ).rejects.toMatchObject({
      name: "LiveTransportError",
      status: 401,
    } satisfies Partial<LiveTransportError>);
  });

  it("rejects an event whose payload ID differs from its SSE cursor", async () => {
    const event = { ...demoSnapshot.live.events[0], id: "43", type: "story-created" };
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(
          streamResponse([`id: 42\nevent: story-created\ndata: ${JSON.stringify(event)}\n\n`]),
        ),
    );
    await expect(
      liveTransport.read("0", new AbortController().signal, vi.fn(), vi.fn()),
    ).rejects.toThrow("identity does not match");
  });

  it("cancels a live response when a malformed event aborts parsing", async () => {
    const cancelled = vi.fn();
    const encoder = new TextEncoder();
    const response = new Response(
      new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(encoder.encode('id: 42\nevent: story-created\ndata: {"id":"43"}\n\n'));
        },
        cancel() {
          cancelled();
        },
      }),
      { headers: { "content-type": "text/event-stream; charset=utf-8" } },
    );
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockResolvedValue(response));

    await expect(
      liveTransport.read("0", new AbortController().signal, vi.fn(), vi.fn()),
    ).rejects.toThrow();
    expect(cancelled).toHaveBeenCalledOnce();
  });
});
