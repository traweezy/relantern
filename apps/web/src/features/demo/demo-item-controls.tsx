"use client";

import { applyItemCommand, type ItemCommand, type ItemState } from "@relantern/domain";
import { memo, useCallback, useEffect, useState } from "react";

const storagePrefix = "relantern.demo.item.v1.";
const initialState: ItemState = { isRead: false, location: "inbox", starred: false };

type DemoItemControlsProps = Readonly<{
  storyID: string;
}>;

const parseStoredState = (encoded: string | null): ItemState => {
  if (encoded === null) {
    return initialState;
  }
  try {
    const value: unknown = JSON.parse(encoded);
    if (typeof value !== "object" || value === null || Array.isArray(value)) {
      return initialState;
    }
    const candidate = value as Record<string, unknown>;
    const location = candidate.location;
    if (
      typeof candidate.isRead !== "boolean" ||
      typeof candidate.starred !== "boolean" ||
      (location !== "inbox" && location !== "later" && location !== "archive")
    ) {
      return initialState;
    }
    return { isRead: candidate.isRead, location, starred: candidate.starred };
  } catch {
    return initialState;
  }
};

const commandFromButton = (button: HTMLButtonElement): ItemCommand | null => {
  switch (button.dataset.command) {
    case "archive":
      return { type: "archive" };
    case "mark-read":
      return { type: "mark-read", value: button.dataset.value === "true" };
    case "move":
      return { type: "move", location: "later" };
    case "star":
      return { type: "star", value: button.dataset.value === "true" };
    default:
      return null;
  }
};

const DemoItemControlsComponent = ({ storyID }: DemoItemControlsProps) => {
  const [state, setState] = useState<ItemState>(initialState);
  const [hydrated, setHydrated] = useState(false);
  const storageKey = `${storagePrefix}${storyID}`;

  useEffect(() => {
    setState(parseStoredState(window.sessionStorage.getItem(storageKey)));
    setHydrated(true);
  }, [storageKey]);

  useEffect(() => {
    if (hydrated) {
      window.sessionStorage.setItem(storageKey, JSON.stringify(state));
    }
  }, [hydrated, state, storageKey]);

  const handleCommand = useCallback((event: React.MouseEvent<HTMLButtonElement>) => {
    const command = commandFromButton(event.currentTarget);
    if (command !== null) {
      setState((current) => applyItemCommand(current, command));
    }
  }, []);

  return (
    <fieldset className="demo-item-controls">
      <legend className="sr-only">Local demo story actions</legend>
      <button
        aria-pressed={state.location === "later"}
        data-command="move"
        onClick={handleCommand}
        type="button"
      >
        {state.location === "later" ? "In Later" : "Save for later"}
      </button>
      <button
        aria-pressed={state.starred}
        data-command="star"
        data-value={String(!state.starred)}
        onClick={handleCommand}
        type="button"
      >
        {state.starred ? "Starred" : "Star"}
      </button>
      <button
        aria-pressed={state.isRead}
        data-command="mark-read"
        data-value={String(!state.isRead)}
        onClick={handleCommand}
        type="button"
      >
        {state.isRead ? "Mark unread" : "Mark read"}
      </button>
      <button data-command="archive" onClick={handleCommand} type="button">
        {state.location === "archive" ? "Archived" : "Archive"}
      </button>
      <span aria-live="polite" className="demo-item-state">
        Local state: {state.location}
        {state.starred ? ", starred" : ""}
        {state.isRead ? ", read" : ", unread"}
      </span>
    </fieldset>
  );
};

export const DemoItemControls = memo<DemoItemControlsProps>(DemoItemControlsComponent);
DemoItemControls.displayName = "DemoItemControls";

export const resetDemoItemState = (): void => {
  for (let index = window.sessionStorage.length - 1; index >= 0; index -= 1) {
    const key = window.sessionStorage.key(index);
    if (key?.startsWith(storagePrefix) === true) {
      window.sessionStorage.removeItem(key);
    }
  }
};
