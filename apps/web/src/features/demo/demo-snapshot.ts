import type { LiveSnapshot, StoryDetail, TodaySnapshot } from "@relantern/domain";
import type { DemoFixtureID } from "./demo-route";

export type DemoRadarDecision = Readonly<{
  confidence: "high" | "medium";
  decision: "adopt" | "assess" | "hold";
  evidence: string;
  technology: string;
}>;

export type DemoSnapshot = Readonly<{
  live: LiveSnapshot;
  methodologyURL: string;
  radar: readonly DemoRadarDecision[];
  stories: Readonly<Record<DemoFixtureID, StoryDetail>>;
  today: TodaySnapshot;
}>;

const deepFreeze = <T>(value: T): T => {
  if (typeof value === "object" && value !== null && !Object.isFrozen(value)) {
    for (const nested of Object.values(value)) {
      deepFreeze(nested);
    }
    Object.freeze(value);
  }
  return value;
};

const reactStory = {
  assertions: [
    {
      claim: "The transition wraps an action so pending UI remains responsive during the update.",
      material: true,
      sources: [
        {
          domain: "react.dev",
          label: "React documentation",
          tier: "T0",
          url: "https://react.dev/reference/react/useTransition",
        },
      ],
    },
    {
      claim: "The migration can remain local to the form boundary before broader adoption.",
      material: false,
      sources: [
        {
          domain: "react.dev",
          label: "React documentation",
          tier: "T0",
          url: "https://react.dev/reference/react/useActionState",
        },
      ],
    },
  ],
  confidence: "high",
  firstSeenAt: "2025-10-14T11:20:00Z",
  headline: "Action-aware transitions simplify pending form states",
  id: "react-actions-transition",
  lastChangedAt: "2025-10-14T14:42:00Z",
  primarySourceUrl: "https://react.dev/reference/react/useActionState",
  readTimeMinutes: 4,
  recommendedAction: "Assess one low-risk form and compare its error and focus behavior.",
  normalizedContent:
    "React action-oriented primitives coordinate pending state, returned errors, and progressive form updates at the form boundary.",
  related: [],
  revisionId: "01990000-0000-7000-8000-000000000001",
  signal: "general",
  sourceCount: 2,
  sourceTier: "T0",
  sources: [
    {
      domain: "react.dev",
      label: "useActionState reference",
      tier: "T0",
      url: "https://react.dev/reference/react/useActionState",
    },
    {
      domain: "react.dev",
      label: "useTransition reference",
      tier: "T0",
      url: "https://react.dev/reference/react/useTransition",
    },
  ],
  status: "new",
  summary:
    "React’s action-oriented primitives provide a smaller coordination surface for pending state, returned errors, and progressive form updates.",
  uncertainties: [
    "The example does not measure the effect on forms with multiple independent submissions.",
  ],
  whyItMatters:
    "The team can remove bespoke pending-state plumbing while preserving keyboard focus and an explicit server boundary.",
} as const satisfies StoryDetail;

const nextStory = {
  assertions: [
    {
      claim:
        "Cache Components make caching an explicit opt-in at the component or function boundary.",
      material: true,
      sources: [
        {
          domain: "nextjs.org",
          label: "Next.js Cache Components guide",
          tier: "T0",
          url: "https://nextjs.org/docs/app/getting-started/cache-components",
        },
      ],
    },
  ],
  confidence: "high",
  firstSeenAt: "2025-10-13T16:05:00Z",
  headline: "Cache Components turn route caching into an explicit boundary",
  id: "next-cache-components",
  lastChangedAt: "2025-10-14T13:28:00Z",
  primarySourceUrl: "https://nextjs.org/docs/app/getting-started/cache-components",
  readTimeMinutes: 5,
  recommendedAction: "Inventory dynamic routes before enabling the cacheComponents flag.",
  normalizedContent:
    "Cache Components make caching an explicit opt-in at a component or function boundary and require deliberate private-data handling.",
  related: [],
  revisionId: "01990000-0000-7000-8000-000000000002",
  signal: "release",
  sourceCount: 2,
  sourceTier: "T0",
  sources: [
    {
      domain: "nextjs.org",
      label: "Cache Components guide",
      tier: "T0",
      url: "https://nextjs.org/docs/app/getting-started/cache-components",
    },
    {
      domain: "nextjs.org",
      label: "Content Security Policy guide",
      tier: "T0",
      url: "https://nextjs.org/docs/app/guides/content-security-policy",
    },
  ],
  status: "updated",
  summary:
    "The route model separates request-time work from cacheable component work, making data freshness decisions visible in code review.",
  uncertainties: [
    "Nonce-based CSP still requires request rendering and excludes a prerendered shell.",
  ],
  whyItMatters:
    "A deliberate cache map avoids accidental private-data caching and makes performance changes easier to verify.",
} as const satisfies StoryDetail;

const postgresStory = {
  assertions: [
    {
      claim:
        "The release adds observability and query-planning improvements worth validating on production-like plans.",
      material: true,
      sources: [
        {
          domain: "postgresql.org",
          label: "PostgreSQL release notes",
          tier: "T0",
          url: "https://www.postgresql.org/docs/release/18.0/",
        },
      ],
    },
  ],
  confidence: "high",
  firstSeenAt: "2025-10-12T09:40:00Z",
  headline: "PostgreSQL 18 expands the upgrade observability surface",
  id: "postgresql-18-observability",
  lastChangedAt: "2025-10-14T12:16:00Z",
  primarySourceUrl: "https://www.postgresql.org/docs/release/18.0/",
  readTimeMinutes: 6,
  recommendedAction: "Run representative plans and restore rehearsal before scheduling an upgrade.",
  normalizedContent:
    "PostgreSQL 18 adds operational and query-planning changes that should be evaluated with representative plans and a restore rehearsal.",
  related: [],
  revisionId: "01990000-0000-7000-8000-000000000003",
  signal: "release",
  sourceCount: 2,
  sourceTier: "T0",
  sources: [
    {
      domain: "postgresql.org",
      label: "PostgreSQL 18 release notes",
      tier: "T0",
      url: "https://www.postgresql.org/docs/release/18.0/",
    },
    {
      domain: "postgresql.org",
      label: "PostgreSQL upgrade documentation",
      tier: "T0",
      url: "https://www.postgresql.org/docs/current/upgrading.html",
    },
  ],
  status: "new",
  summary:
    "The major release changes operational behavior as well as features, so the useful unit of evaluation is an upgrade rehearsal rather than a checklist.",
  uncertainties: ["Extension compatibility remains workload-specific."],
  whyItMatters:
    "The database is the durability boundary; upgrade confidence depends on measured plans, extensions, and recovery evidence.",
} as const satisfies StoryDetail;

const goStory = {
  assertions: [
    {
      claim:
        "Toolchain security releases should be treated as rebuild triggers even when application code is unchanged.",
      material: true,
      sources: [
        {
          domain: "go.dev",
          label: "Go security policy",
          tier: "T0",
          url: "https://go.dev/security/policy",
        },
      ],
    },
  ],
  confidence: "high",
  firstSeenAt: "2025-10-14T08:10:00Z",
  headline: "Go toolchain advisories require artifact-level rebuild evidence",
  id: "go-toolchain-security",
  lastChangedAt: "2025-10-14T15:04:00Z",
  primarySourceUrl: "https://go.dev/security/policy",
  readTimeMinutes: 3,
  recommendedAction: "Rebuild affected binaries, scan the images, and retain SBOM evidence.",
  normalizedContent:
    "Toolchain security releases require affected binaries to be rebuilt and verified against the corrected compiler and standard library.",
  related: [],
  revisionId: "01990000-0000-7000-8000-000000000004",
  signal: "security",
  sourceCount: 2,
  sourceTier: "T0",
  sources: [
    {
      domain: "go.dev",
      label: "Go security policy",
      tier: "T0",
      url: "https://go.dev/security/policy",
    },
    {
      domain: "pkg.go.dev",
      label: "Go vulnerability database",
      tier: "T0",
      url: "https://pkg.go.dev/vuln/",
    },
  ],
  status: "updated",
  summary:
    "A compiler or standard-library fix is only operationally complete after the release artifact is rebuilt and verified against the new toolchain.",
  uncertainties: ["Applicability depends on the packages and platforms present in each binary."],
  whyItMatters:
    "Source-only remediation can leave already-built containers exposed and creates a false sense of completion.",
} as const satisfies StoryDetail;

const storySummaries = [goStory, reactStory, nextStory, postgresStory].map(
  ({
    assertions: _assertions,
    normalizedContent: _normalizedContent,
    related: _related,
    revisionId: _revisionId,
    sources: _sources,
    uncertainties: _uncertainties,
    ...story
  }) => story,
);

export const demoSnapshot = deepFreeze({
  live: {
    events: storySummaries.map((story, index) => ({
      id: `${story.id}:demo-${index + 1}`,
      observedAt: story.lastChangedAt,
      story,
      type: story.status === "updated" ? "story-updated" : "story-created",
    })),
    generatedAt: "2025-10-14T15:15:00Z",
  },
  methodologyURL: "https://github.com/traweezy/relantern",
  radar: [
    {
      confidence: "high",
      decision: "assess",
      evidence: "Strong primary documentation; migration evidence is still local-only.",
      technology: "React action primitives",
    },
    {
      confidence: "medium",
      decision: "hold",
      evidence: "CSP and request-rendering constraints need route-by-route validation.",
      technology: "Broad Cache Components adoption",
    },
    {
      confidence: "high",
      decision: "adopt",
      evidence: "Artifact rebuild and SBOM verification close the toolchain advisory loop.",
      technology: "Toolchain-triggered rebuilds",
    },
  ],
  stories: {
    "go-toolchain-security": { ...goStory, related: [reactStory, nextStory] },
    "next-cache-components": { ...nextStory, related: [reactStory, postgresStory] },
    "postgresql-18-observability": { ...postgresStory, related: [goStory, nextStory] },
    "react-actions-transition": { ...reactStory, related: [nextStory, goStory] },
  },
  today: {
    coverageEndAt: "2025-10-14T15:15:00Z",
    coverageStartAt: "2025-10-13T15:15:00Z",
    deliveryState: "delivered",
    generatedAt: "2025-10-14T15:15:00Z",
    nextRunAt: "2025-10-15T12:00:00Z",
    stats: {
      criticalAlerts: 1,
      estimatedCostUsd: "1.82",
      releases: 2,
      reviewRequired: 1,
      sourceCoverage: 96,
    },
    stories: storySummaries,
  },
} as const satisfies DemoSnapshot);
