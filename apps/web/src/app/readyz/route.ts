import "server-only";

import { connection } from "next/server";
import { getIntelligenceAPIConfiguration } from "@/server/intelligence/config";
import { createReadinessProbe } from "@/server/readiness";

const noStoreHeaders = { "Cache-Control": "no-store" } as const;
const probe = createReadinessProbe({
  baseURL: () => getIntelligenceAPIConfiguration().baseURL,
  environment: () => process.env,
  now: () => performance.now(),
  onFailure: (reason) => console.warn("web readiness check failed", { reason }),
  request: (url, options) => fetch(url, options),
});

export const GET = async (): Promise<Response> => {
  await connection();
  const result = await probe();
  if (!result.ready) {
    return Response.json(
      {
        detail: "A required private dependency is unavailable.",
        instance: "/readyz",
        status: 503,
        title: "Service Unavailable",
        type: "about:blank",
      },
      {
        headers: {
          ...noStoreHeaders,
          "Content-Type": "application/problem+json",
        },
        status: 503,
      },
    );
  }

  return Response.json({ service: "web", status: "ready" }, { headers: noStoreHeaders });
};
