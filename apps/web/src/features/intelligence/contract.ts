import type {
  ClaimEvidence,
  LiveEvent,
  LiveSnapshot,
  SourceTier,
  StoryDetail,
  StorySignal,
  StorySource,
  StoryStatus,
  StorySummary,
  TodaySnapshot,
} from "@relantern/domain";

export class IntelligenceContractError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "IntelligenceContractError";
  }
}

const isRecord = (value: unknown): value is Readonly<Record<string, unknown>> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const recordValue = (value: unknown, name: string): Readonly<Record<string, unknown>> => {
  if (!isRecord(value)) {
    throw new IntelligenceContractError(`${name} must be an object`);
  }
  return value;
};

const stringValue = (value: unknown, name: string, maximum = 4096): string => {
  if (typeof value !== "string" || value.length === 0 || value.length > maximum) {
    throw new IntelligenceContractError(`${name} must be a bounded non-empty string`);
  }
  return value;
};

const nullableString = (value: unknown, name: string): string | null => {
  if (value === null) {
    return null;
  }
  return stringValue(value, name);
};

const integerValue = (value: unknown, name: string, maximum = 1_000_000): number => {
  if (!Number.isSafeInteger(value) || (value as number) < 0 || (value as number) > maximum) {
    throw new IntelligenceContractError(`${name} must be a bounded non-negative integer`);
  }
  return value as number;
};

const timestampValue = (value: unknown, name: string): string => {
  const timestamp = stringValue(value, name, 64);
  if (!Number.isFinite(Date.parse(timestamp))) {
    throw new IntelligenceContractError(`${name} must be an RFC 3339 timestamp`);
  }
  return timestamp;
};

const enumValue = <T extends string>(value: unknown, name: string, allowed: ReadonlySet<T>): T => {
  if (typeof value !== "string" || !allowed.has(value as T)) {
    throw new IntelligenceContractError(`${name} contains an unsupported value`);
  }
  return value as T;
};

const confidenceValues = new Set(["high", "medium", "low", "unknown"] as const);
const sourceTierValues = new Set(["T0", "T1", "T2", "T3"] as const);
const statusValues = new Set(["new", "updated"] as const);
const signalValues = new Set([
  "breaking-change",
  "deprecation",
  "general",
  "release",
  "security",
] as const);

const sourceURLValue = (value: unknown, name: string): string => {
  const url = stringValue(value, name);
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    throw new IntelligenceContractError(`${name} must be an absolute URL`);
  }
  const localHTTP =
    parsed.protocol === "http:" &&
    (parsed.hostname === "127.0.0.1" ||
      parsed.hostname === "localhost" ||
      parsed.hostname === "fake-source");
  if (parsed.protocol !== "https:" && !localHTTP) {
    throw new IntelligenceContractError(`${name} must use HTTPS outside local fixtures`);
  }
  return parsed.toString();
};

const parseSource = (value: unknown, name: string): StorySource => {
  const source = recordValue(value, name);
  return {
    domain: stringValue(source.domain, `${name}.domain`, 253),
    label: stringValue(source.label, `${name}.label`, 253),
    tier: enumValue<SourceTier>(source.tier, `${name}.tier`, sourceTierValues),
    url: sourceURLValue(source.url, `${name}.url`),
  };
};

export const parseStorySummary = (value: unknown, name = "story"): StorySummary => {
  const story = recordValue(value, name);
  return {
    confidence: enumValue(story.confidence, `${name}.confidence`, confidenceValues),
    firstSeenAt: timestampValue(story.firstSeenAt, `${name}.firstSeenAt`),
    headline: stringValue(story.headline, `${name}.headline`, 180),
    id: stringValue(story.id, `${name}.id`, 80),
    lastChangedAt: timestampValue(story.lastChangedAt, `${name}.lastChangedAt`),
    primarySourceUrl: sourceURLValue(story.primarySourceUrl, `${name}.primarySourceUrl`),
    readTimeMinutes: integerValue(story.readTimeMinutes, `${name}.readTimeMinutes`, 120),
    recommendedAction: stringValue(story.recommendedAction, `${name}.recommendedAction`, 1500),
    signal: enumValue<StorySignal>(story.signal, `${name}.signal`, signalValues),
    sourceCount: integerValue(story.sourceCount, `${name}.sourceCount`, 10_000),
    sourceTier: enumValue<SourceTier>(story.sourceTier, `${name}.sourceTier`, sourceTierValues),
    status: enumValue<StoryStatus>(story.status, `${name}.status`, statusValues),
    summary: stringValue(story.summary, `${name}.summary`, 2000),
    whyItMatters: stringValue(story.whyItMatters, `${name}.whyItMatters`, 2000),
  };
};

const parseClaimEvidence = (value: unknown, name: string): ClaimEvidence => {
  const claim = recordValue(value, name);
  if (typeof claim.material !== "boolean" || !Array.isArray(claim.sources)) {
    throw new IntelligenceContractError(`${name} has an invalid evidence shape`);
  }
  return {
    claim: stringValue(claim.claim, `${name}.claim`, 1000),
    material: claim.material,
    sources: claim.sources.map((source, index) => parseSource(source, `${name}.sources[${index}]`)),
  };
};

export const parseStoryDetail = (value: unknown): StoryDetail => {
  const story = recordValue(value, "story");
  if (
    !Array.isArray(story.assertions) ||
    !Array.isArray(story.related) ||
    !Array.isArray(story.sources) ||
    !Array.isArray(story.uncertainties)
  ) {
    throw new IntelligenceContractError("story detail collections must be arrays");
  }
  return {
    ...parseStorySummary(story),
    assertions: story.assertions.map((claim, index) =>
      parseClaimEvidence(claim, `story.assertions[${index}]`),
    ),
    normalizedContent: stringValue(story.normalizedContent, "story.normalizedContent", 1_000_000),
    related: story.related.map((related, index) =>
      parseStorySummary(related, `story.related[${index}]`),
    ),
    revisionId: stringValue(story.revisionId, "story.revisionId", 36),
    sources: story.sources.map((source, index) => parseSource(source, `story.sources[${index}]`)),
    uncertainties: story.uncertainties.map((uncertainty, index) =>
      stringValue(uncertainty, `story.uncertainties[${index}]`, 1000),
    ),
  };
};

export const parseTodaySnapshot = (value: unknown): TodaySnapshot => {
  const snapshot = recordValue(value, "today");
  const stats = recordValue(snapshot.stats, "today.stats");
  if (!Array.isArray(snapshot.stories)) {
    throw new IntelligenceContractError("today.stories must be an array");
  }
  return {
    coverageEndAt: timestampValue(snapshot.coverageEndAt, "today.coverageEndAt"),
    coverageStartAt: timestampValue(snapshot.coverageStartAt, "today.coverageStartAt"),
    deliveryState: enumValue(
      snapshot.deliveryState,
      "today.deliveryState",
      new Set(["delivered", "pending", "unavailable"] as const),
    ),
    generatedAt: timestampValue(snapshot.generatedAt, "today.generatedAt"),
    nextRunAt: nullableString(snapshot.nextRunAt, "today.nextRunAt"),
    stats: {
      criticalAlerts: integerValue(stats.criticalAlerts, "today.stats.criticalAlerts"),
      estimatedCostUsd: stringValue(stats.estimatedCostUsd, "today.stats.estimatedCostUsd", 40),
      releases: integerValue(stats.releases, "today.stats.releases"),
      reviewRequired: integerValue(stats.reviewRequired, "today.stats.reviewRequired"),
      sourceCoverage: integerValue(stats.sourceCoverage, "today.stats.sourceCoverage", 100),
    },
    stories: snapshot.stories.map((story, index) =>
      parseStorySummary(story, `today.stories[${index}]`),
    ),
  };
};

const parseLiveEvent = (value: unknown, index: number): LiveEvent => {
  const event = recordValue(value, `live.events[${index}]`);
  return {
    id: stringValue(event.id, `live.events[${index}].id`, 180),
    observedAt: timestampValue(event.observedAt, `live.events[${index}].observedAt`),
    story: parseStorySummary(event.story, `live.events[${index}].story`),
    type: enumValue(
      event.type,
      `live.events[${index}].type`,
      new Set(["story-created", "story-updated"] as const),
    ),
  };
};

export const parseLiveSnapshot = (value: unknown): LiveSnapshot => {
  const snapshot = recordValue(value, "live");
  if (!Array.isArray(snapshot.events)) {
    throw new IntelligenceContractError("live.events must be an array");
  }
  return {
    events: snapshot.events.map(parseLiveEvent),
    generatedAt: timestampValue(snapshot.generatedAt, "live.generatedAt"),
  };
};
