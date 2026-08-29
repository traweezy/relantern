import { isDemoFixtureID } from "./demo-route";
import { demoSnapshot } from "./demo-snapshot";

export const getDemoSnapshot = () => demoSnapshot;

export const getDemoStory = (fixtureID: string) =>
  isDemoFixtureID(fixtureID) ? demoSnapshot.stories[fixtureID] : null;
