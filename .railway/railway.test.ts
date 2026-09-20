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
  it.each(["staging", "production"] as const)("pins the %s topology", async (environment) => {
    const evaluated = await evaluate(environment);
    const services = servicesByName(
      evaluated.graph.resources.filter((item) => item.type === "service"),
    );
    expect([...services.keys()].sort()).toEqual(["api", "migrate", "postgres", "web", "worker"]);
    expect(
      evaluated.graph.resources.filter((item) => item.type === "bucket").map((item) => item.name),
    ).toEqual(["bucket"]);
    expect(
      evaluated.graph.resources
        .filter((item) => item.type === "volume")
        .map((item) => ({ config: item.config, name: item.name })),
    ).toEqual([
      {
        config: {
          region: "us-east4-eqdc4a",
          sizeMB: 5_000,
        },
        name: "postgres-data",
      },
    ]);

    expect(services.get("postgres")?.source?.image).toContain("@sha256:");
    expect(services.get("postgres")?.networking).toBeUndefined();
    expect(services.get("api")?.networking).toBeUndefined();
    expect(services.get("worker")?.networking).toBeUndefined();
    expect(services.get("migrate")?.networking).toBeUndefined();
  });

  it("deploys staging only after GitHub checks", async () => {
    const evaluated = await evaluate("staging");
    const services = servicesByName(
      evaluated.graph.resources.filter((item) => item.type === "service"),
    );
    for (const name of ["api", "migrate", "web", "worker"]) {
      expect(services.get(name)?.source).toMatchObject({
        branch: "staging",
        checkSuites: true,
        repo: "traweezy/relantern",
        type: "github",
      });
    }
  });

  it("disconnects production from mutable branch deployments", async () => {
    const evaluated = await evaluate("production");
    const services = servicesByName(
      evaluated.graph.resources.filter((item) => item.type === "service"),
    );
    for (const name of ["api", "migrate", "web", "worker"]) {
      expect(services.get(name)?.kind).toBe("empty");
      expect(services.get(name)?.source).toEqual({ type: "empty" });
    }
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

  it("preserves attended sealed secret copies at every consumer", async () => {
    const evaluated = await evaluate("staging");
    const services = servicesByName(
      evaluated.graph.resources.filter((item) => item.type === "service"),
    );
    expect(services.get("postgres")?.variables).toMatchObject({
      DATABASE_URL: { type: "preserve" },
      POSTGRES_PASSWORD: { type: "preserve" },
    });
    expect(services.get("api")?.variables).toMatchObject({
      DATABASE_URL: { type: "preserve" },
      OPENAI_API_KEY: { type: "preserve" },
      OPENAI_PROJECT_ID: { type: "preserve" },
      WEB_INTERNAL_SERVICE_TOKEN: { type: "preserve" },
    });
    expect(services.get("worker")?.variables).toMatchObject({
      DATABASE_URL: { type: "preserve" },
      OPENAI_API_KEY: { type: "preserve" },
    });
    expect(services.get("migrate")?.variables).toMatchObject({
      DATABASE_URL: { type: "preserve" },
    });
    expect(services.get("web")?.variables).toMatchObject({
      BETTER_AUTH_SECRET: { type: "preserve" },
      DATABASE_URL: { type: "preserve" },
      GITHUB_OAUTH_CLIENT_ID: { type: "preserve" },
      GITHUB_OAUTH_CLIENT_SECRET: { type: "preserve" },
      OPENAI_WEBHOOK_SECRET: { type: "preserve" },
      WEB_INTERNAL_SERVICE_TOKEN: { type: "preserve" },
    });
  });
});
