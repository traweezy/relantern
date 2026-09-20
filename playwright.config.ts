import { defineConfig, devices } from "@playwright/test";

const demoPort = Number(process.env.RELANTERN_E2E_DEMO_PORT ?? "3100");
if (!Number.isInteger(demoPort) || demoPort < 1 || demoPort > 65535) {
  throw new Error("RELANTERN_E2E_DEMO_PORT must be a valid TCP port");
}
const demoURL = `http://127.0.0.1:${demoPort}`;
const privateURL = process.env.RELANTERN_E2E_PRIVATE_URL ?? "http://127.0.0.1:3000";
const runInCI = process.env.CI === "true";

export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  retries: runInCI ? 1 : 0,
  workers: runInCI ? 2 : 1,
  reporter: runInCI ? [["list"], ["github"]] : "list",
  use: {
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  projects: [
    {
      name: "demo",
      testMatch: "demo.spec.ts",
      use: { ...devices["Desktop Chrome"], baseURL: demoURL },
    },
    {
      name: "private",
      testMatch: "private.spec.ts",
      use: { ...devices["Desktop Chrome"], baseURL: privateURL },
    },
  ],
  ...(process.env.RELANTERN_E2E_START_DEMO === "true"
    ? {
        webServer: {
          command: `PORT=${demoPort} node .local/public-demo/server.mjs`,
          url: `${demoURL}/healthz`,
          reuseExistingServer: false,
          timeout: 30_000,
        },
      }
    : {}),
});
