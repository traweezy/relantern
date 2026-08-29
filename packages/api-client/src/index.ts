import createClient from "openapi-fetch";
import type { paths } from "./generated/schema";

export type ApiClientOptions = Readonly<{
  baseUrl: string;
  fetch?: typeof globalThis.fetch;
}>;

export const createApiClient = ({ baseUrl, fetch }: ApiClientOptions) =>
  createClient<paths>({
    baseUrl,
    ...(fetch === undefined ? {} : { fetch }),
  });
