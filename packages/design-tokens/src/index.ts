export const motionDurations = {
  content: 200,
  feedback: 100,
  panel: 160,
} as const;

export const spacing = {
  compact: 8,
  control: 12,
  section: 24,
} as const;

export const layout = {
  desktopNavigation: 240,
  desktopStoryListMax: 520,
  desktopStoryListMin: 420,
  readingMeasure: 82,
  touchTarget: 44,
} as const;

export const performanceBudgets = {
  demoInitialJavaScriptKiB: 180,
  todayInitialJavaScriptKiB: 220,
  virtualizeAfterRows: 200,
} as const;

export const colorRoles = {
  accent: "accent",
  border: "border",
  danger: "danger",
  ink: "ink",
  mutedInk: "ink-muted",
  surface: "surface",
  surfaceRaised: "surface-raised",
} as const;

export const themeColors = {
  accent: "oklch(0.78 0.145 175)",
  accentWarm: "oklch(0.79 0.145 73)",
  border: "oklch(0.31 0.027 249)",
  danger: "oklch(0.69 0.17 28)",
  info: "oklch(0.72 0.12 235)",
  ink: "oklch(0.95 0.012 245)",
  inkMuted: "oklch(0.72 0.025 245)",
  surface: "oklch(0.145 0.018 252)",
  surfaceRaised: "oklch(0.195 0.022 252)",
  warning: "oklch(0.79 0.145 73)",
} as const;
