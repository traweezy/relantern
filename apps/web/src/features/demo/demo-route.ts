export const demoFixtureIDs = [
  "go-toolchain-security",
  "next-cache-components",
  "postgresql-18-observability",
  "react-actions-transition",
] as const;

export type DemoFixtureID = (typeof demoFixtureIDs)[number];

const demoFixtureIDSet = new Set<string>(demoFixtureIDs);
const demoStoryPathPattern = /^\/demo\/story\/([a-z0-9](?:[a-z0-9-]{0,78}[a-z0-9])?)$/;

export const isDemoFixtureID = (value: string): value is DemoFixtureID =>
  demoFixtureIDSet.has(value);

export const isPublicDemoPath = (pathname: string): boolean => {
  if (pathname === "/demo" || pathname === "/demo/opengraph-image") {
    return true;
  }
  const match = demoStoryPathPattern.exec(pathname);
  return match?.[1] !== undefined;
};
