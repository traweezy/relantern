"use client";

import { memo, useCallback, useMemo, useState } from "react";
import { StoryCard } from "@/features/intelligence/story-card";
import { DemoItemControls, resetDemoItemState } from "./demo-item-controls";
import type { DemoSnapshot } from "./demo-snapshot";

type DemoView = "live" | "ops" | "radar" | "today";

type DemoWorkspaceProps = Readonly<{
  snapshot: DemoSnapshot;
}>;

const views: readonly Readonly<{ label: string; value: DemoView }>[] = [
  { label: "Today", value: "today" },
  { label: "Live", value: "live" },
  { label: "Radar", value: "radar" },
  { label: "Sources + Ops", value: "ops" },
];

const guideCopy = [
  "Start with the highest-value story in the morning brief.",
  "Open its claim-to-source evidence instead of trusting a summary alone.",
  "Triage it locally to Later or Starred without touching a real account.",
  "Compare the evidence with the explicit technology-radar decision.",
] as const;

const DemoWorkspaceComponent = ({ snapshot }: DemoWorkspaceProps) => {
  const [guideStep, setGuideStep] = useState(-1);
  const [view, setView] = useState<DemoView>("today");
  const representative = snapshot.stories["go-toolchain-security"];
  const liveStories = useMemo(
    () => snapshot.live.events.map((event) => event.story),
    [snapshot.live.events],
  );

  const handleView = useCallback((event: React.MouseEvent<HTMLButtonElement>) => {
    const nextView = event.currentTarget.dataset.view as DemoView | undefined;
    if (nextView !== undefined) {
      setView(nextView);
    }
  }, []);

  const handleGuideStart = useCallback(() => {
    setGuideStep(0);
    setView("today");
  }, []);

  const handleGuideNext = useCallback(() => {
    setGuideStep((current) => {
      const next = current + 1;
      if (next === 3) {
        setView("radar");
      }
      return next >= guideCopy.length ? -1 : next;
    });
  }, []);

  const handleReset = useCallback(() => {
    resetDemoItemState();
    setGuideStep(-1);
    setView("today");
    window.location.reload();
  }, []);

  return (
    <div className="demo-workspace">
      <header className="demo-hero">
        <div>
          <p className="eyebrow">Evidence-first developer intelligence</p>
          <h1>Know what changed. See why it matters. Keep the proof attached.</h1>
          <p>
            Relantern turns primary technical sources into a private, action-oriented brief without
            hiding the claim-to-evidence chain.
          </p>
          <div className="demo-hero-actions">
            <button className="demo-primary-button" onClick={handleGuideStart} type="button">
              Start guided demo
            </button>
            <a href={`/demo/story/${representative.id}`}>Inspect representative story</a>
          </div>
        </div>
        <div className="demo-value-card">
          <div className="story-badges">
            <span className="signal-badge signal-security">Security</span>
            <span className="source-tier">T0 · 2 sources</span>
          </div>
          <strong>{representative.headline}</strong>
          <p>{representative.whyItMatters}</p>
          <a href={`/demo/story/${representative.id}`}>Inspect claim evidence →</a>
        </div>
      </header>

      <nav aria-label="Demo surfaces" className="demo-tabs">
        {views.map((item) => (
          <button
            aria-current={view === item.value ? "page" : undefined}
            data-view={item.value}
            key={item.value}
            onClick={handleView}
            type="button"
          >
            {item.label}
          </button>
        ))}
        <button className="demo-reset" onClick={handleReset} type="button">
          Reset demo
        </button>
      </nav>

      {guideStep >= 0 && (
        <section aria-live="polite" className="demo-guide">
          <div>
            <span>
              Guided step {guideStep + 1} of {guideCopy.length}
            </span>
            <p>{guideCopy[guideStep]}</p>
          </div>
          <button onClick={handleGuideNext} type="button">
            {guideStep === guideCopy.length - 1 ? "Finish tour" : "Next step"}
          </button>
        </section>
      )}

      {view === "today" && (
        <section aria-labelledby="demo-today-title" className="demo-surface">
          <div className="collection-toolbar">
            <div>
              <p className="eyebrow">Illustrative snapshot · 14 Oct 2025</p>
              <h2 id="demo-today-title">Today</h2>
            </div>
            <div className="demo-stat-line">
              <span>{snapshot.today.stats.criticalAlerts} critical</span>
              <span>{snapshot.today.stats.releases} releases</span>
              <span>{snapshot.today.stats.sourceCoverage}% coverage</span>
            </div>
          </div>
          <div className="story-list">
            {snapshot.today.stories.map((story) => (
              <div className="demo-story-row" key={story.id}>
                <StoryCard href={`/demo/story/${story.id}`} story={story} />
                <DemoItemControls storyID={story.id} />
              </div>
            ))}
          </div>
        </section>
      )}

      {view === "live" && (
        <section aria-labelledby="demo-live-title" className="demo-surface">
          <div className="collection-toolbar">
            <div>
              <p className="eyebrow">Fixture event stream</p>
              <h2 id="demo-live-title">Live</h2>
            </div>
            <p>No network connection is opened in this demonstration.</p>
          </div>
          <div className="story-list">
            {liveStories.map((story) => (
              <StoryCard href={`/demo/story/${story.id}`} key={story.id} story={story} />
            ))}
          </div>
        </section>
      )}

      {view === "radar" && (
        <section aria-labelledby="demo-radar-title" className="demo-surface">
          <div className="collection-toolbar">
            <div>
              <p className="eyebrow">Owner judgment stays explicit</p>
              <h2 id="demo-radar-title">Technology radar</h2>
            </div>
          </div>
          <div className="radar-grid">
            {snapshot.radar.map((decision) => (
              <article key={decision.technology}>
                <span className={`radar-decision radar-${decision.decision}`}>
                  {decision.decision}
                </span>
                <h3>{decision.technology}</h3>
                <p>{decision.evidence}</p>
                <small>{decision.confidence} confidence · illustrative owner decision</small>
              </article>
            ))}
          </div>
        </section>
      )}

      {view === "ops" && (
        <section aria-labelledby="demo-ops-title" className="demo-surface">
          <div className="collection-toolbar">
            <div>
              <p className="eyebrow">Trust is operational</p>
              <h2 id="demo-ops-title">Sources + operations</h2>
            </div>
          </div>
          <div className="ops-grid">
            <article>
              <span>Source health</span>
              <strong>24 / 25</strong>
              <p>Enabled primary sources reporting within their expected freshness window.</p>
            </article>
            <article>
              <span>Evidence checks</span>
              <strong>18 / 19</strong>
              <p>Material assertions linked to a reviewed source span.</p>
            </article>
            <article>
              <span>Monthly AI cost</span>
              <strong>${snapshot.today.stats.estimatedCostUsd}</strong>
              <p>Illustrative ledger remains below its synthetic soft budget.</p>
            </article>
          </div>
        </section>
      )}
    </div>
  );
};

export const DemoWorkspace = memo<DemoWorkspaceProps>(DemoWorkspaceComponent);
DemoWorkspace.displayName = "DemoWorkspace";
