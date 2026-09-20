import "server-only";

type EnvironmentValues = Readonly<Record<string, string | undefined>>;

export type ReadinessFailureReason =
  | "invalid-environment"
  | "invalid-release-sha"
  | "invalid-private-api-configuration"
  | "private-api-unavailable"
  | "private-api-unready"
  | "invalid-private-api-response"
  | "release-sha-mismatch";

export type ReadinessResult =
  | Readonly<{ ready: true }>
  | Readonly<{ ready: false; reason: ReadinessFailureReason }>;

type ReadinessDependencies = Readonly<{
  baseURL: () => string;
  environment: () => EnvironmentValues;
  now: () => number;
  onFailure: (reason: ReadinessFailureReason) => void;
  request: typeof fetch;
}>;

const fullGitSHA = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
const probeTimeoutMilliseconds = 2_500;
const readyTTLMilliseconds = 2_000;
const unavailableTTLMilliseconds = 1_000;

const failure = (reason: ReadinessFailureReason): ReadinessResult => ({ ready: false, reason });

const checkReadiness = async (dependencies: ReadinessDependencies): Promise<ReadinessResult> => {
  const environment = dependencies.environment();
  const appEnvironment = environment.APP_ENV?.trim() ?? "local";
  if (!["local", "test", "staging", "production"].includes(appEnvironment)) {
    return failure("invalid-environment");
  }

  const hosted = appEnvironment === "staging" || appEnvironment === "production";
  const expectedGitSHA = environment.GIT_SHA?.trim();
  if (hosted && (expectedGitSHA === undefined || !fullGitSHA.test(expectedGitSHA))) {
    return failure("invalid-release-sha");
  }

  let baseURL: string;
  try {
    baseURL = dependencies.baseURL();
  } catch {
    return failure("invalid-private-api-configuration");
  }

  let response: Response;
  try {
    response = await dependencies.request(new URL("/readyz", baseURL), {
      cache: "no-store",
      headers: { accept: "application/json" },
      redirect: "error",
      signal: AbortSignal.timeout(probeTimeoutMilliseconds),
    });
  } catch {
    return failure("private-api-unavailable");
  }

  if (!response.ok) {
    return failure("private-api-unready");
  }

  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    return failure("invalid-private-api-response");
  }

  if (
    typeof payload !== "object" ||
    payload === null ||
    Array.isArray(payload) ||
    !("status" in payload) ||
    payload.status !== "ready"
  ) {
    return failure("invalid-private-api-response");
  }
  if (hosted && (!("gitSha" in payload) || payload.gitSha !== expectedGitSHA)) {
    return failure("release-sha-mismatch");
  }

  return { ready: true };
};

export const createReadinessProbe = (
  dependencies: ReadinessDependencies,
): (() => Promise<ReadinessResult>) => {
  let cachedResult: ReadinessResult | undefined;
  let expiresAt = 0;
  let inFlight: Promise<ReadinessResult> | undefined;

  return () => {
    if (cachedResult !== undefined && dependencies.now() < expiresAt) {
      return Promise.resolve(cachedResult);
    }
    if (inFlight !== undefined) {
      return inFlight;
    }

    inFlight = checkReadiness(dependencies)
      .then((result) => {
        cachedResult = result;
        expiresAt =
          dependencies.now() + (result.ready ? readyTTLMilliseconds : unavailableTTLMilliseconds);
        if (!result.ready) {
          dependencies.onFailure(result.reason);
        }
        return result;
      })
      .finally(() => {
        inFlight = undefined;
      });
    return inFlight;
  };
};
