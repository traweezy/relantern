import "server-only";

type EnvironmentValues = Readonly<Record<string, string | undefined>>;

export class PublicConfigurationError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "PublicConfigurationError";
  }
}

export const loadPublicBaseURL = (environment: EnvironmentValues): URL => {
  const appEnvironment = environment.APP_ENV?.trim() || "local";
  const configured = environment.PUBLIC_BASE_URL?.trim();
  if (configured === undefined || configured === "") {
    if (appEnvironment !== "local" && appEnvironment !== "test") {
      throw new PublicConfigurationError("PUBLIC_BASE_URL is required outside local and test");
    }
    return new URL("http://127.0.0.1:3000");
  }
  let parsed: URL;
  try {
    parsed = new URL(configured);
  } catch {
    throw new PublicConfigurationError("PUBLIC_BASE_URL must be an absolute URL");
  }
  if (
    (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.pathname !== "/" ||
    parsed.search !== "" ||
    parsed.hash !== ""
  ) {
    throw new PublicConfigurationError(
      "PUBLIC_BASE_URL must be an HTTP(S) origin without credentials, path, query, or fragment",
    );
  }
  const localHTTP =
    parsed.protocol === "http:" &&
    (appEnvironment === "local" || appEnvironment === "test") &&
    (parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost");
  if (parsed.protocol !== "https:" && !localHTTP) {
    throw new PublicConfigurationError("PUBLIC_BASE_URL must use HTTPS outside local and test");
  }
  return parsed;
};

export const getPublicBaseURL = (): URL => loadPublicBaseURL(process.env);
