import type { LiveEvent, LiveSnapshot } from "@relantern/domain";
import { createParser } from "eventsource-parser";
import { parseLiveEvent, parseLiveSnapshot } from "./contract";

const cursorPattern = /^(0|[1-9][0-9]{0,18})$/;
const maximumEventCharacters = 128_000;

export class LiveTransportError extends Error {
  public readonly status: number;

  public constructor(message: string, status = 0) {
    super(message);
    this.name = "LiveTransportError";
    this.status = status;
  }
}

export type LiveTransport = Readonly<{
  read: (
    cursor: string,
    signal: AbortSignal,
    onEvent: (event: LiveEvent) => void,
    onConnected: () => void,
  ) => Promise<"ended" | "reset">;
  snapshot: (signal: AbortSignal) => Promise<LiveSnapshot>;
}>;

export const liveTransport: LiveTransport = {
  read: async (cursor, signal, onEvent, onConnected) => {
    if (!cursorPattern.test(cursor)) {
      throw new LiveTransportError("The live cursor is invalid.");
    }
    const response = await fetch(`/api/intelligence/live?after=${encodeURIComponent(cursor)}`, {
      cache: "no-store",
      credentials: "same-origin",
      headers: { accept: "text/event-stream" },
      signal,
    });
    if (!response.ok) {
      throw new LiveTransportError("The live stream request failed.", response.status);
    }
    if (
      !response.body ||
      !(response.headers.get("content-type") ?? "").startsWith("text/event-stream")
    ) {
      throw new LiveTransportError("The live stream response is invalid.");
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let reset = false;
    const parser = createParser({
      maxBufferSize: maximumEventCharacters,
      onError: () => {
        throw new LiveTransportError("The live stream exceeded its event limit or was malformed.");
      },
      onEvent: (message) => {
        if (message.event === "reset_required") {
          reset = true;
          return;
        }
        if (message.event !== "story-created" && message.event !== "story-updated") {
          return;
        }
        if (message.id === undefined || !cursorPattern.test(message.id)) {
          throw new LiveTransportError("The live stream event has no valid cursor.");
        }
        let decoded: unknown;
        try {
          decoded = JSON.parse(message.data);
        } catch {
          throw new LiveTransportError("The live stream event is invalid JSON.");
        }
        const event = parseLiveEvent(decoded, 0);
        if (event.id !== message.id || event.type !== message.event) {
          throw new LiveTransportError("The live stream event identity does not match its cursor.");
        }
        onEvent(event);
      },
    });
    let finished = false;
    try {
      onConnected();
      for (;;) {
        const { done, value } = await reader.read();
        if (done) {
          parser.feed(decoder.decode());
          finished = true;
          return reset ? "reset" : "ended";
        }
        parser.feed(decoder.decode(value, { stream: true }));
        if (reset) {
          await reader.cancel();
          finished = true;
          return "reset";
        }
      }
    } finally {
      if (!finished) {
        // A malformed event must release its upstream SSE connection before
        // reconnecting, even when the stream body has not ended.
        try {
          await reader.cancel();
        } catch {
          // Preserve the parsing or network error that entered this path.
        }
      }
      reader.releaseLock();
    }
  },
  snapshot: async (signal) => {
    const response = await fetch("/api/intelligence/live/snapshot", {
      cache: "no-store",
      credentials: "same-origin",
      headers: { accept: "application/json" },
      signal,
    });
    if (!response.ok) {
      throw new LiveTransportError("The live snapshot request failed.", response.status);
    }
    const encoded = await response.text();
    if (encoded.length > 2_000_000) {
      throw new LiveTransportError("The live snapshot exceeded its size limit.");
    }
    let decoded: unknown;
    try {
      decoded = JSON.parse(encoded);
    } catch {
      throw new LiveTransportError("The live snapshot is invalid JSON.");
    }
    return parseLiveSnapshot(decoded);
  },
};
