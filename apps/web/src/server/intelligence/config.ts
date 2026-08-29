import "server-only";

import { readFileSync } from "node:fs";

type EnvironmentValues = Readonly<Record<string, string | undefined>>;
type SecretReader = (path: string) => string;

export type IntelligenceAPIConfiguration = Readonly<{
  baseURL: string;
  serviceToken: string;
}>;

export class IntelligenceConfigurationError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "IntelligenceConfigurationError";
  }
}

const value = (environment: EnvironmentValues, name: string): string | undefined => {
  const configured = environment[name]?.trim();
  return configured === "" ? undefined : configured;
};

const loadServiceToken = (environment: EnvironmentValues, readSecret: SecretReader): string => {
  const direct = value(environment, "WEB_INTERNAL_SERVICE_TOKEN");
  const path = value(environment, "WEB_INTERNAL_SERVICE_TOKEN_FILE");
  if (direct !== undefined && path !== undefined) {
    throw new IntelligenceConfigurationError(
      "WEB_INTERNAL_SERVICE_TOKEN and WEB_INTERNAL_SERVICE_TOKEN_FILE are mutually exclusive",
    );
  }
  let token = direct;
  if (path !== undefined) {
    try {
      token = readSecret(path).trim();
    } catch {
      throw new IntelligenceConfigurationError("WEB_INTERNAL_SERVICE_TOKEN_FILE could not be read");
    }
  }
  if (token === undefined || token.length < 32 || token.length > 512) {
    throw new IntelligenceConfigurationError(
      "WEB_INTERNAL_SERVICE_TOKEN must contain between 32 and 512 characters",
    );
  }
  return token;
};

export const loadIntelligenceAPIConfiguration = (
  environment: EnvironmentValues,
  readSecret: SecretReader = (path) => readFileSync(path, "utf8"),
): IntelligenceAPIConfiguration => {
  const rawURL = value(environment, "INTERNAL_API_URL") ?? "http://127.0.0.1:8080";
  let parsed: URL;
  try {
    parsed = new URL(rawURL);
  } catch {
    throw new IntelligenceConfigurationError("INTERNAL_API_URL must be an absolute URL");
  }
  if (
    (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.hash !== "" ||
    parsed.search !== "" ||
    parsed.pathname !== "/"
  ) {
    throw new IntelligenceConfigurationError(
      "INTERNAL_API_URL must be an HTTP(S) origin without credentials, path, query, or fragment",
    );
  }
  const appEnvironment = value(environment, "APP_ENV") ?? "local";
  const privateServiceHost =
    parsed.hostname === "api" || parsed.hostname.endsWith(".railway.internal");
  const localLoopbackHost =
    appEnvironment === "local" &&
    (parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost");
  if (parsed.protocol !== "https:" && !privateServiceHost && !localLoopbackHost) {
    throw new IntelligenceConfigurationError(
      "INTERNAL_API_URL must use HTTPS outside the local private network",
    );
  }
  return {
    baseURL: parsed.origin,
    serviceToken: loadServiceToken(environment, readSecret),
  };
};

export const getIntelligenceAPIConfiguration = (): IntelligenceAPIConfiguration =>
  loadIntelligenceAPIConfiguration(process.env);
