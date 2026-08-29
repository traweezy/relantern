"use client";

import { performanceBudgets } from "@relantern/design-tokens";
import type { LiveEvent, LiveSnapshot, StorySignal } from "@relantern/domain";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { parseLiveSnapshot } from "./contract";
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
  const [status, setStatus] = useState<"connecting" | "live" | "reconnecting">("connecting");
  const pausedRef = useRef(paused);
  const pendingEventsRef = useRef<readonly LiveEvent[]>([]);

  useEffect(() => {
    pausedRef.current = paused;
  }, [paused]);

  useEffect(() => {
    const source = new EventSource("/api/intelligence/live");
    const handleOpen = () => setStatus("live");
    const handleError = () => setStatus("reconnecting");
    const handleSnapshot = (event: MessageEvent<string>) => {
      let snapshot: LiveSnapshot;
      try {
        snapshot = parseLiveSnapshot(JSON.parse(event.data));
      } catch {
        return;
      }
      if (pausedRef.current) {
        pendingEventsRef.current = mergeEvents(pendingEventsRef.current, snapshot.events);
        setPendingCount(pendingEventsRef.current.length);
        return;
      }
      setEvents((current) => mergeEvents(current, snapshot.events));
    };
    source.addEventListener("open", handleOpen);
    source.addEventListener("error", handleError);
    source.addEventListener("snapshot", handleSnapshot as EventListener);
    return () => {
      source.removeEventListener("open", handleOpen);
      source.removeEventListener("error", handleError);
      source.removeEventListener("snapshot", handleSnapshot as EventListener);
      source.close();
    };
  }, []);

  const visibleEvents = useMemo(
    () => (filter === "all" ? events : events.filter((event) => event.story.signal === filter)),
    [events, filter],
  );

  const handlePause = useCallback(() => {
    setPaused((current) => {
      const next = !current;
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
          {paused ? "Paused" : status}
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
            <StoryCard href={`/story/${event.story.id}`} story={event.story} timezone={timezone} />
          </div>
        ))}
      </div>
    </>
  );
};

export const LiveStream = memo<LiveStreamProps>(LiveStreamComponent);
LiveStream.displayName = "LiveStream";
