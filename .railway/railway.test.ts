import type { ServiceNode } from "railway/iac";
import { evaluateRailwayProject } from "railway/iac";
import { describe, expect, it } from "vitest";

const evaluate = async (environment: "production" | "staging") =>
  evaluateRailwayProject({
    context: { environment },
    file: ".railway/railway.ts",
  });

const servicesByName = (services: ReadonlyArray<ServiceNode>) =>
  new Map(services.map((service) => [service.name, service]));

describe("Railway infrastructure contract", () => {
  it.each([
    ["staging", "staging"],
    ["production", "master"],
  ] as const)("pins the %s topology and source branch", async (environment, branch) => {
    const evaluated = await evaluate(environment);
    const services = servicesByName(
      evaluated.graph.resources.filter((item) => item.type === "service"),
    );
    expect([...services.keys()].sort()).toEqual(["api", "migrate", "postgres", "web", "worker"]);
    expect(
      evaluated.graph.resources.filter((item) => item.type === "bucket").map((item) => item.name),
    ).toEqual(["bucket"]);
    expect(
      evaluated.graph.resources.filter((item) => item.type === "volume").map((item) => item.name),
    ).toEqual(["postgres-data"]);

    for (const name of ["api", "migrate", "web", "worker"]) {
      expect(services.get(name)?.source).toMatchObject({
        branch,
        checkSuites: true,
        repo: "traweezy/relantern",
        type: "github",
      });
    }
    expect(services.get("postgres")?.source?.image).toContain("@sha256:");
    expect(services.get("postgres")?.networking).toBeUndefined();
    expect(services.get("api")?.networking).toBeUndefined();
    expect(services.get("worker")?.networking).toBeUndefined();
    expect(services.get("migrate")?.networking).toBeUndefined();
  });

  it("keeps production delivery and AI activation disabled", async () => {
    const evaluated = await evaluate("production");
    const services = servicesByName(
      evaluated.graph.resources.filter((item) => item.type === "service"),
    );
    expect(services.get("worker")?.variables).toMatchObject({
      ALLOW_LIVE_DELIVERY: { type: "literal", value: "false" },
      DELIVERY_MODE: { type: "literal", value: "disabled" },
      DISCORD_ENABLED: { type: "literal", value: "false" },
      OPENAI_FAST_ENABLED: { type: "literal", value: "false" },
      OPENAI_RESEARCH_ENABLED: { type: "literal", value: "false" },
      SEARCH_HYBRID_ENABLED: { type: "literal", value: "false" },
    });
    expect(services.get("api")?.variables).toMatchObject({
      SEARCH_HYBRID_ENABLED: { type: "literal", value: "false" },
    });
  });

  it("requires an isolated staging delivery and low-cap OpenAI profile", async () => {
    const evaluated = await evaluate("staging");
    const services = servicesByName(
      evaluated.graph.resources.filter((item) => item.type === "service"),
    );
    expect(services.get("worker")?.variables).toMatchObject({
      ALLOW_LIVE_DELIVERY: { type: "literal", value: "true" },
      DELIVERY_MODE: { type: "literal", value: "live" },
      DISCORD_ENABLED: { type: "literal", value: "true" },
      DISCORD_WEBHOOK_URL: { type: "preserve" },
      OPENAI_FAST_ENABLED: { type: "literal", value: "true" },
      OPENAI_MONTHLY_HARD_USD: { type: "literal", value: "15.00" },
      OPENAI_MONTHLY_SOFT_USD: { type: "literal", value: "8.00" },
      OPENAI_RESEARCH_ENABLED: { type: "literal", value: "true" },
      SEARCH_HYBRID_ENABLED: { type: "literal", value: "true" },
    });
    expect(services.get("api")?.variables).toMatchObject({
      OPENAI_API_KEY: { type: "preserve" },
      OPENAI_BASE_URL: { type: "literal", value: "https://api.openai.com" },
      OPENAI_PROJECT_ID: { type: "preserve" },
      SEARCH_HYBRID_ENABLED: { type: "literal", value: "true" },
    });
  });

  it("generates internal secrets while preserving external credentials", async () => {
    const evaluated = await evaluate("staging");
    const services = servicesByName(
      evaluated.graph.resources.filter((item) => item.type === "service"),
    );
    expect(services.get("api")?.variables?.WEB_INTERNAL_SERVICE_TOKEN).toMatchObject({
      type: "raw",
      value: { isSealed: true },
    });
    expect(services.get("web")?.variables).toMatchObject({
      BETTER_AUTH_SECRET: { type: "raw", value: { isSealed: true } },
      GITHUB_OAUTH_CLIENT_ID: { type: "preserve" },
      GITHUB_OAUTH_CLIENT_SECRET: { type: "preserve" },
      OPENAI_WEBHOOK_SECRET: { type: "raw", value: { isSealed: true } },
    });
  });
});
