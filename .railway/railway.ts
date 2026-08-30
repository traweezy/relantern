import type { VariableConfig } from "railway/iac";
import {
  bucket,
  defineRailway,
  empty,
  github,
  image,
  preserve,
  ref,
  service,
  volume,
} from "railway/iac";

const repository = "traweezy/relantern";
const applicationRegion = "us-east4-eqdc4a";
const bucketRegion = "iad";
const postgresImage =
  "pgvector/pgvector:0.8.6-pg18-trixie@sha256:78bf48b801e792f99e3ac62b5036fd3876e9be48afda16c1e331af1c75ceb2ff";
const secretAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
const railwayVariable = (name: string): string => ["$", "{{", name, "}}"].join("");
const railwayHTTPSOrigin = (name: string): string => `https://${railwayVariable(name)}`;

const generatedSecret = (description: string): VariableConfig => ({
  description,
  generator: `secret(64, "${secretAlphabet}")`,
  isOptional: false,
  isSealed: true,
});

const commonEnvironment = (environment: "production" | "staging") => ({
  APP_ENV: environment,
  APP_VERSION: `0.0.0-${environment}`,
  CLOCK_MODE: "system",
  GIT_SHA: railwayVariable("RAILWAY_GIT_COMMIT_SHA"),
  LOG_LEVEL: "info",
  PROVIDER_REQUEST_TIMEOUT: "5s",
  SHUTDOWN_TIMEOUT: "15s",
  SOURCE_FIXTURES_PATH: "/app/sources/fixtures.yaml",
  SOURCE_REGISTRY_PATH: "/app/sources/registry.yaml",
  TEST_NOW: "",
});

export default defineRailway((context, project) => {
  if (!context.isEnvironment("staging") && !context.isEnvironment("production")) {
    throw new Error("Railway IaC is restricted to staging and production");
  }

  const environment = context.isEnvironment("production") ? "production" : "staging";
  const production = environment === "production";
  const source = production
    ? empty()
    : github(repository, {
        branch: "staging",
        checkSuites: true,
        rootDirectory: "/",
      });
  const region = { [applicationRegion]: 1 };
  const postgresData = volume("postgres-data", { region: applicationRegion });
  const rawEvidence = bucket("bucket", { region: bucketRegion });

  const postgres = service("postgres", {
    source: image(postgresImage, {
      autoUpdates: { type: "disabled", tagMode: "sha" },
    }),
    deploy: {
      drainingSeconds: 30,
      restartPolicyType: "ALWAYS",
    },
    env: {
      DATABASE_URL: `postgresql://${railwayVariable("POSTGRES_USER")}:${railwayVariable("POSTGRES_PASSWORD")}@${railwayVariable("RAILWAY_PRIVATE_DOMAIN")}:5432/${railwayVariable("POSTGRES_DB")}?sslmode=disable`,
      PGDATA: "/var/lib/postgresql/data/pgdata",
      POSTGRES_DB: "relantern",
      POSTGRES_PASSWORD: generatedSecret("Generated per-environment PostgreSQL credential."),
      POSTGRES_USER: "relantern",
    },
    regions: region,
    volumeMounts: {
      "/var/lib/postgresql/data": postgresData,
    },
  });

  const api = service("api", {
    source,
    build: {
      builder: "DOCKERFILE",
      dockerfilePath: "deploy/docker/api.Dockerfile",
      watchPatterns: [
        "cmd/api/**",
        "contracts/**",
        "deploy/docker/api.Dockerfile",
        "go.mod",
        "go.sum",
        "internal/**",
      ],
    },
    deploy: {
      drainingSeconds: 30,
      healthcheckPath: "/readyz",
      healthcheckTimeout: 180,
      overlapSeconds: 30,
      restartPolicyMaxRetries: 10,
      restartPolicyType: "ON_FAILURE",
    },
    env: {
      ...commonEnvironment(environment),
      DATABASE_MAX_CONNS: "10",
      DATABASE_MIN_CONNS: "1",
      DATABASE_URL: postgres.env.DATABASE_URL,
      EMBEDDING_DIMENSIONS: "1536",
      HTTP_PORT: "8080",
      OPENAI_API_KEY: preserve(),
      OPENAI_BASE_URL: "https://api.openai.com",
      OPENAI_EMBEDDING_MODEL: "text-embedding-3-small",
      OPENAI_ORG_ID: preserve(),
      OPENAI_PROJECT_ID: preserve(),
      SEARCH_HYBRID_ENABLED: production ? "false" : "true",
      SEARCH_RRF_K: "60",
      WEB_INTERNAL_SERVICE_TOKEN: generatedSecret(
        "Generated shared credential for authenticated private API traffic.",
      ),
    },
    regions: region,
  });

  const worker = service("worker", {
    source,
    build: {
      builder: "DOCKERFILE",
      dockerfilePath: "deploy/docker/worker.Dockerfile",
      watchPatterns: [
        "cmd/worker/**",
        "deploy/docker/worker.Dockerfile",
        "go.mod",
        "go.sum",
        "internal/**",
        "sources/**",
      ],
    },
    deploy: {
      drainingSeconds: 45,
      healthcheckPath: "/healthz",
      healthcheckTimeout: 180,
      overlapSeconds: 30,
      restartPolicyMaxRetries: 10,
      restartPolicyType: "ON_FAILURE",
    },
    env: {
      ...commonEnvironment(environment),
      ALLOW_LIVE_DELIVERY: production ? "false" : "true",
      CLUSTER_MAX_AGE: "720h",
      DATABASE_MAX_CONNS: "10",
      DATABASE_MIN_CONNS: "1",
      DATABASE_URL: postgres.env.DATABASE_URL,
      DEDUPE_EMBEDDING_THRESHOLD: "0.86",
      DEDUPE_SIMHASH_DISTANCE: "17",
      DELIVERY_MODE: production ? "disabled" : "live",
      DELIVERY_REQUEST_TIMEOUT: "10s",
      DISCORD_ENABLED: production ? "false" : "true",
      DISCORD_WEBHOOK_URL: preserve(),
      EMBEDDING_DIMENSIONS: "1536",
      HTTP_PORT: "8081",
      OBJECT_STORAGE_ACCESS_KEY: ref(rawEvidence, "ACCESS_KEY_ID"),
      OBJECT_STORAGE_BUCKET: ref(rawEvidence, "BUCKET"),
      OBJECT_STORAGE_ENDPOINT: ref(rawEvidence, "ENDPOINT"),
      OBJECT_STORAGE_REGION: ref(rawEvidence, "REGION"),
      OBJECT_STORAGE_SECRET_KEY: ref(rawEvidence, "SECRET_ACCESS_KEY"),
      OPENAI_API_KEY: api.env.OPENAI_API_KEY,
      OPENAI_BACKGROUND_ENABLED: "true",
      OPENAI_BASE_URL: "https://api.openai.com",
      OPENAI_DAILY_WEB_SEARCH_LIMIT: production ? "20" : "40",
      OPENAI_EMBEDDING_MODEL: "text-embedding-3-small",
      OPENAI_FAST_ENABLED: production ? "false" : "true",
      OPENAI_FAST_MAX_OUTPUT_TOKENS: "4096",
      OPENAI_FAST_REASONING: "low",
      OPENAI_MAX_DOCUMENT_AGE: "720h",
      OPENAI_MODEL_FAST: "gpt-5.6-luna",
      OPENAI_MODEL_RESEARCH: "gpt-5.6-terra",
      OPENAI_MONTHLY_HARD_USD: production ? "50.00" : "15.00",
      OPENAI_MONTHLY_SOFT_USD: production ? "25.00" : "8.00",
      OPENAI_ORG_ID: api.env.OPENAI_ORG_ID,
      OPENAI_PROJECT_ID: api.env.OPENAI_PROJECT_ID,
      OPENAI_RESEARCH_ENABLED: production ? "false" : "true",
      OPENAI_RESEARCH_MAX_OUTPUT_TOKENS: "8192",
      OPENAI_RESEARCH_MAX_TOOL_CALLS: "4",
      OPENAI_RESEARCH_REASONING: "medium",
      OPENAI_VERBOSITY: "low",
      PUBLIC_BASE_URL: railwayHTTPSOrigin("web.RAILWAY_PUBLIC_DOMAIN"),
      RESEND_ENABLED: "false",
      RIVER_AI_TIMEOUT: "10m",
      RIVER_RESEARCH_TIMEOUT: "30m",
      SCHEDULER_RECONCILE_INTERVAL: "1m",
      SEARCH_HYBRID_ENABLED: production ? "false" : "true",
      SEARCH_RRF_K: "60",
    },
    regions: region,
  });

  const web = service("web", {
    source,
    build: {
      builder: "DOCKERFILE",
      dockerfilePath: "deploy/docker/web.Dockerfile",
      watchPatterns: [
        "apps/web/**",
        "deploy/docker/web.Dockerfile",
        "package.json",
        "packages/**",
        "pnpm-lock.yaml",
        "pnpm-workspace.yaml",
      ],
    },
    deploy: {
      drainingSeconds: 30,
      healthcheckPath: "/healthz",
      healthcheckTimeout: 180,
      overlapSeconds: 30,
      restartPolicyMaxRetries: 10,
      restartPolicyType: "ON_FAILURE",
    },
    env: {
      ALLOW_LIVE_EXTERNAL_APIS: "true",
      APP_ENV: environment,
      AUTH_ALLOWED_GITHUB_USER_ID: "5276132",
      AUTH_PROVIDER_MODE: "github",
      BETTER_AUTH_SECRET: generatedSecret("Generated per-environment owner session secret."),
      BETTER_AUTH_URL: railwayHTTPSOrigin("RAILWAY_PUBLIC_DOMAIN"),
      DATABASE_URL: postgres.env.DATABASE_URL,
      GITHUB_OAUTH_CLIENT_ID: preserve(),
      GITHUB_OAUTH_CLIENT_SECRET: preserve(),
      INTERNAL_API_URL: `http://${railwayVariable("api.RAILWAY_PRIVATE_DOMAIN")}:8080`,
      NODE_ENV: "production",
      OPENAI_WEBHOOK_SECRET: generatedSecret(
        "Generated per-environment OpenAI webhook signing secret.",
      ),
      OWNER_TIMEZONE: "America/New_York",
      PORT: "3000",
      PUBLIC_BASE_URL: railwayHTTPSOrigin("RAILWAY_PUBLIC_DOMAIN"),
      SESSION_MAX_AGE_SECONDS: "604800",
      WEB_INTERNAL_SERVICE_TOKEN: api.env.WEB_INTERNAL_SERVICE_TOKEN,
    },
    regions: region,
  });

  const migrate = service("migrate", {
    source,
    build: {
      builder: "DOCKERFILE",
      dockerfilePath: "deploy/docker/migrate.Dockerfile",
      watchPatterns: [
        "cmd/migrate/**",
        "deploy/docker/migrate.Dockerfile",
        "go.mod",
        "go.sum",
        "internal/**",
        "migrations/**",
        "sources/**",
      ],
    },
    deploy: {
      restartPolicyType: "NEVER",
      startCommand: "/app/migrate up",
    },
    env: {
      ...commonEnvironment(environment),
      DATABASE_MAX_CONNS: "1",
      DATABASE_MIN_CONNS: "0",
      DATABASE_URL: postgres.env.DATABASE_URL,
    },
    regions: region,
  });

  return project("Relantern", {
    environments: ["staging", "production"],
    resources: [postgresData, rawEvidence, postgres, migrate, api, worker, web],
  });
});
