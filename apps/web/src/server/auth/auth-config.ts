import "server-only";

import { readFileSync } from "node:fs";

type Environment = "local" | "production" | "staging" | "test";
type ProviderMode = "fixture" | "github";

type EnvironmentValues = Readonly<Record<string, string | undefined>>;
type SecretReader = (path: string) => string;

export type AuthConfiguration = Readonly<{
  allowedGitHubUserID: string;
  baseURL: string;
  databaseURL: string;
  environment: Environment;
  oauthAuthorizationURL?: string;
  oauthClientID: string;
  oauthClientSecret: string;
  oauthTokenURL?: string;
  oauthUserInfoURL?: string;
  ownerTimezone: string;
  providerMode: ProviderMode;
  secret: string;
  secureCookies: boolean;
  sessionMaxAgeSeconds: number;
}>;

const supportedEnvironments = new Set<Environment>(["local", "test", "staging", "production"]);
const numericGitHubIDPattern = /^[1-9][0-9]{0,15}$/;

export class AuthConfigurationError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "AuthConfigurationError";
  }
}

const value = (environment: EnvironmentValues, name: string): string | undefined => {
  const configured = environment[name]?.trim();
  return configured === "" ? undefined : configured;
};

const requiredValue = (environment: EnvironmentValues, name: string): string => {
  const configured = value(environment, name);
  if (configured === undefined) {
    throw new AuthConfigurationError(`${name} is required`);
  }
  return configured;
};

const secretValue = (
  environment: EnvironmentValues,
  valueName: string,
  fileName: string,
  readSecret: SecretReader,
): string => {
  const direct = value(environment, valueName);
  const path = value(environment, fileName);
  if (direct !== undefined && path !== undefined) {
    throw new AuthConfigurationError(`${valueName} and ${fileName} are mutually exclusive`);
  }
  let secret = direct;
  if (path !== undefined) {
    try {
      secret = readSecret(path).trim();
    } catch {
      throw new AuthConfigurationError(`${fileName} could not be read`);
    }
  }
  if (secret === undefined || secret.length < 32 || secret.length > 4096) {
    throw new AuthConfigurationError(`${valueName} must contain between 32 and 4096 characters`);
  }
  return secret;
};

const absoluteURL = (
  raw: string,
  name: string,
  environment: Environment,
  allowedLocalHosts: ReadonlySet<string> = new Set(["127.0.0.1", "localhost"]),
): URL => {
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    throw new AuthConfigurationError(`${name} must be an absolute URL`);
  }
  const localHTTP =
    parsed.protocol === "http:" &&
    (environment === "local" || environment === "test") &&
    allowedLocalHosts.has(parsed.hostname);
  if (parsed.protocol !== "https:" && !localHTTP) {
    throw new AuthConfigurationError(`${name} must use HTTPS outside a local fixture boundary`);
  }
  if (parsed.username !== "" || parsed.password !== "" || parsed.hash !== "") {
    throw new AuthConfigurationError(`${name} may not contain credentials or a fragment`);
  }
  return parsed;
};

const loadDatabaseURL = (environment: EnvironmentValues, readSecret: SecretReader): string => {
  const configured = value(environment, "DATABASE_URL");
  if (configured !== undefined) {
    let parsed: URL;
    try {
      parsed = new URL(configured);
    } catch {
      throw new AuthConfigurationError("DATABASE_URL must be an absolute PostgreSQL URL");
    }
    if (parsed.protocol !== "postgres:" && parsed.protocol !== "postgresql:") {
      throw new AuthConfigurationError("DATABASE_URL must use the postgres or postgresql scheme");
    }
    return parsed.toString();
  }

  const databasePassword = secretValue(
    environment,
    "DATABASE_PASSWORD",
    "DATABASE_PASSWORD_FILE",
    readSecret,
  );
  const databaseHost = value(environment, "DATABASE_HOST") ?? "127.0.0.1:5432";
  const databaseName = value(environment, "DATABASE_NAME") ?? "relantern";
  const databaseUser = value(environment, "DATABASE_USER") ?? "relantern";
  const databaseSSLMode = value(environment, "DATABASE_SSLMODE") ?? "disable";
  const parsed = new URL(`postgresql://${databaseHost}/${encodeURIComponent(databaseName)}`);
  parsed.username = databaseUser;
  parsed.password = databasePassword;
  parsed.searchParams.set("sslmode", databaseSSLMode);
  return parsed.toString();
};

const parseEnvironment = (environment: EnvironmentValues): Environment => {
  const configured = (value(environment, "APP_ENV") ?? "local") as Environment;
  if (!supportedEnvironments.has(configured)) {
    throw new AuthConfigurationError("APP_ENV must be local, test, staging, or production");
  }
  return configured;
};

const parseSessionMaxAge = (environment: EnvironmentValues): number => {
  const raw = value(environment, "SESSION_MAX_AGE_SECONDS") ?? "604800";
  const parsed = Number(raw);
  if (!Number.isSafeInteger(parsed) || parsed < 3600 || parsed > 2_592_000) {
    throw new AuthConfigurationError(
      "SESSION_MAX_AGE_SECONDS must be an integer from 3600 through 2592000",
    );
  }
  return parsed;
};

const parseTimezone = (environment: EnvironmentValues): string => {
  const timezone = value(environment, "OWNER_TIMEZONE") ?? "America/New_York";
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: timezone }).format();
  } catch {
    throw new AuthConfigurationError("OWNER_TIMEZONE must be a valid IANA timezone");
  }
  return timezone;
};

export const loadAuthConfiguration = (
  environment: EnvironmentValues,
  readSecret: SecretReader = (path) => readFileSync(path, "utf8"),
): AuthConfiguration => {
  const appEnvironment = parseEnvironment(environment);
  const baseURL = absoluteURL(
    value(environment, "BETTER_AUTH_URL") ?? "http://127.0.0.1:3000",
    "BETTER_AUTH_URL",
    appEnvironment,
  );
  if (baseURL.pathname !== "/" || baseURL.search !== "") {
    throw new AuthConfigurationError("BETTER_AUTH_URL must be an origin without a path or query");
  }

  const allowedGitHubUserID = requiredValue(environment, "AUTH_ALLOWED_GITHUB_USER_ID");
  if (
    !numericGitHubIDPattern.test(allowedGitHubUserID) ||
    !Number.isSafeInteger(Number(allowedGitHubUserID))
  ) {
    throw new AuthConfigurationError("AUTH_ALLOWED_GITHUB_USER_ID must be a positive numeric ID");
  }

  const defaultProviderMode =
    appEnvironment === "local" || appEnvironment === "test" ? "fixture" : "github";
  const providerMode = (value(environment, "AUTH_PROVIDER_MODE") ??
    defaultProviderMode) as ProviderMode;
  if (providerMode !== "fixture" && providerMode !== "github") {
    throw new AuthConfigurationError("AUTH_PROVIDER_MODE must be fixture or github");
  }
  if (providerMode === "fixture" && appEnvironment !== "local" && appEnvironment !== "test") {
    throw new AuthConfigurationError("the fixture OAuth provider is restricted to local and test");
  }
  if (
    providerMode === "github" &&
    (appEnvironment === "local" || appEnvironment === "test") &&
    value(environment, "ALLOW_LIVE_EXTERNAL_APIS") !== "true"
  ) {
    throw new AuthConfigurationError(
      "local live GitHub OAuth requires ALLOW_LIVE_EXTERNAL_APIS=true",
    );
  }

  const secret = secretValue(
    environment,
    "BETTER_AUTH_SECRET",
    "BETTER_AUTH_SECRET_FILE",
    readSecret,
  );
  const databaseURL = loadDatabaseURL(environment, readSecret);
  const common = {
    allowedGitHubUserID,
    baseURL: baseURL.origin,
    databaseURL,
    environment: appEnvironment,
    ownerTimezone: parseTimezone(environment),
    providerMode,
    secret,
    secureCookies: baseURL.protocol === "https:",
    sessionMaxAgeSeconds: parseSessionMaxAge(environment),
  } as const;

  if (providerMode === "github") {
    return {
      ...common,
      oauthClientID: requiredValue(environment, "GITHUB_OAUTH_CLIENT_ID"),
      oauthClientSecret: secretValue(
        environment,
        "GITHUB_OAUTH_CLIENT_SECRET",
        "GITHUB_OAUTH_CLIENT_SECRET_FILE",
        readSecret,
      ),
    };
  }

  const fixtureHosts = new Set(["127.0.0.1", "localhost", "fake-source"]);
  return {
    ...common,
    oauthAuthorizationURL: absoluteURL(
      value(environment, "LOCAL_OAUTH_AUTHORIZATION_URL") ??
        "http://127.0.0.1:8090/oauth/authorize",
      "LOCAL_OAUTH_AUTHORIZATION_URL",
      appEnvironment,
      fixtureHosts,
    ).toString(),
    oauthClientID: "relantern-local",
    oauthClientSecret: secretValue(
      environment,
      "LOCAL_OAUTH_STUB_SECRET",
      "LOCAL_OAUTH_STUB_SECRET_FILE",
      readSecret,
    ),
    oauthTokenURL: absoluteURL(
      value(environment, "LOCAL_OAUTH_TOKEN_URL") ?? "http://127.0.0.1:8090/oauth/token",
      "LOCAL_OAUTH_TOKEN_URL",
      appEnvironment,
      fixtureHosts,
    ).toString(),
    oauthUserInfoURL: absoluteURL(
      value(environment, "LOCAL_OAUTH_USERINFO_URL") ?? "http://127.0.0.1:8090/oauth/userinfo",
      "LOCAL_OAUTH_USERINFO_URL",
      appEnvironment,
      fixtureHosts,
    ).toString(),
  };
};

let cachedConfiguration: AuthConfiguration | undefined;

export const getAuthConfiguration = (): AuthConfiguration => {
  cachedConfiguration ??= loadAuthConfiguration(process.env);
  return cachedConfiguration;
};
