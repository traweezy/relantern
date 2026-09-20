"use client";

import { performanceBudgets } from "@relantern/design-tokens";
import type { LiveEvent, LiveSnapshot, StorySignal } from "@relantern/domain";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EmptyState } from "./empty-state";
import { LiveTransportError, liveTransport } from "./live-transport";
import { StoryCard } from "./story-card";

type LiveStreamProps = Readonly<{
  initialSnapshot: LiveSnapshot;
  timezone: string;
}>;

type SignalFilter = "all" | StorySignal;

const filters: readonly Readonly<{ label: string; value: SignalFilter }>[] = [
  { label: "All signal", value: "all" },
  { label: "Security", value: "security" },
  { label: "Releases", value: "release" },
  { label: "Breaking", value: "breaking-change" },
  { label: "Deprecations", value: "deprecation" },
];

const mergeEvents = (
  current: readonly LiveEvent[],
  incoming: readonly LiveEvent[],
): LiveEvent[] => {
  const events = new Map(current.map((event) => [event.id, event]));
  for (const event of incoming) {
    events.set(event.id, event);
  }
  return [...events.values()]
    .sort((left, right) => Date.parse(right.observedAt) - Date.parse(left.observedAt))
    .slice(0, performanceBudgets.virtualizeAfterRows);
};

const LiveStreamComponent = ({ initialSnapshot, timezone }: LiveStreamProps) => {
  const [events, setEvents] = useState<readonly LiveEvent[]>(initialSnapshot.events);
  const [filter, setFilter] = useState<SignalFilter>("all");
  const [paused, setPaused] = useState(false);
  const [pendingCount, setPendingCount] = useState(0);
  const [status, setStatus] = useState<"connecting" | "live" | "reconnecting" | "sign-in-required">(
    "connecting",
  );
  const pausedRef = useRef(paused);
  const pendingEventsRef = useRef<readonly LiveEvent[]>([]);
  const cursorRef = useRef(initialSnapshot.cursor);
  const queuedEventsRef = useRef<readonly LiveEvent[]>([]);
  const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    pausedRef.current = paused;
  }, [paused]);

  useEffect(() => {
    let active = true;
    let controller: AbortController | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let failures = 0;

    const flushEvents = () => {
      flushTimerRef.current = null;
      const queued = queuedEventsRef.current;
      queuedEventsRef.current = [];
      if (queued.length === 0) {
        return;
      }
      if (pausedRef.current) {
        pendingEventsRef.current = mergeEvents(pendingEventsRef.current, queued);
        setPendingCount(pendingEventsRef.current.length);
      } else {
        setEvents((current) => mergeEvents(current, queued));
      }
    };
    const queueEvent = (event: LiveEvent) => {
      cursorRef.current = event.id;
      queuedEventsRef.current = [...queuedEventsRef.current, event];
      flushTimerRef.current ??= setTimeout(flushEvents, 100);
    };
    const scheduleRetry = (delay: number) => {
      if (active && !document.hidden) {
        retryTimer = setTimeout(() => void connect(), delay);
      }
    };
    const connect = async () => {
      if (!active || document.hidden) {
        return;
      }
      const connectionController = new AbortController();
      controller = connectionController;
      setStatus(failures === 0 ? "connecting" : "reconnecting");
      try {
        const outcome = await liveTransport.read(
          cursorRef.current,
          connectionController.signal,
          queueEvent,
          () => setStatus("live"),
        );
        if (!active || document.hidden) {
          return;
        }
        failures = 0;
        if (outcome === "reset") {
          const snapshot = await liveTransport.snapshot(connectionController.signal);
          cursorRef.current = snapshot.cursor;
          queuedEventsRef.current = [];
          pendingEventsRef.current = [];
          setPendingCount(0);
          setEvents(snapshot.events);
        }
        setStatus("reconnecting");
        scheduleRetry(250);
      } catch (error: unknown) {
        if (!active || document.hidden || connectionController.signal.aborted) {
          return;
        }
        if (error instanceof LiveTransportError && (error.status === 401 || error.status === 403)) {
          setStatus("sign-in-required");
          return;
        }
        failures += 1;
        setStatus("reconnecting");
        const ceiling = Math.min(30_000, 1_000 * 2 ** Math.min(failures - 1, 5));
        scheduleRetry(Math.round(ceiling * (0.75 + Math.random() * 0.5)));
      }
    };
    const handleVisibility = () => {
      if (document.hidden) {
        controller?.abort();
        if (retryTimer !== null) {
          clearTimeout(retryTimer);
          retryTimer = null;
        }
        setStatus("reconnecting");
      } else {
        failures = 0;
        void connect();
      }
    };
    document.addEventListener("visibilitychange", handleVisibility);
    void connect();
    return () => {
      active = false;
      document.removeEventListener("visibilitychange", handleVisibility);
      controller?.abort();
      if (retryTimer !== null) {
        clearTimeout(retryTimer);
      }
      if (flushTimerRef.current !== null) {
        clearTimeout(flushTimerRef.current);
      }
    };
  }, []);

  const visibleEvents = useMemo(
    () => (filter === "all" ? events : events.filter((event) => event.story.signal === filter)),
    [events, filter],
  );

  const handlePause = useCallback(() => {
    setPaused((current) => {
      const next = !current;
      pausedRef.current = next;
      if (!next && pendingEventsRef.current.length > 0) {
        setEvents((existing) => mergeEvents(existing, pendingEventsRef.current));
        pendingEventsRef.current = [];
        setPendingCount(0);
      }
      return next;
    });
  }, []);

  const handleFilter = useCallback((event: React.ChangeEvent<HTMLSelectElement>) => {
    setFilter(event.currentTarget.value as SignalFilter);
  }, []);

  return (
    <>
      <div className="live-toolbar">
        <div className="live-status" role="status">
          <span aria-hidden="true" className={`live-dot live-dot-${status}`} />
          {paused ? "Paused" : status === "sign-in-required" ? "Sign in required" : status}
        </div>
        <label>
          <span>Filter stream</span>
          <select onChange={handleFilter} value={filter}>
            {filters.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
        <button className="stream-control" onClick={handlePause} type="button">
          {paused ? "Resume stream" : "Pause stream"}
        </button>
      </div>
      {paused && pendingCount > 0 && (
        <button className="new-events-banner" onClick={handlePause} type="button">
          {pendingCount} new {pendingCount === 1 ? "event" : "events"} · Resume without losing your
          place
        </button>
      )}
      {visibleEvents.length === 0 ? (
        <EmptyState
          detail="Material events will appear here after evidence checks pass. The stream remains connected while you read."
          eyebrow="Listening"
          title="No events in the current window"
        />
      ) : (
        <div aria-live="polite" className="story-list live-story-list">
          {visibleEvents.map((event) => (
            <div className="live-event" key={event.id}>
              <div className="live-event-rail">
                <span />
                <time dateTime={event.observedAt}>
                  {new Intl.DateTimeFormat("en-US", {
                    hour: "numeric",
                    minute: "2-digit",
                    timeZone: timezone,
                  }).format(new Date(event.observedAt))}
                </time>
              </div>
              <StoryCard
                href={`/story/${event.story.id}`}
                story={event.story}
                timezone={timezone}
              />
            </div>
          ))}
        </div>
      )}
    </>
  );
};

export const LiveStream = memo<LiveStreamProps>(LiveStreamComponent);
LiveStream.displayName = "LiveStream";
