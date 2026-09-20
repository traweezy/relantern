import { readFileSync } from "node:fs";
import { basename, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const serviceNames = ["api", "migrate", "postgres", "web", "worker"];
const gitServices = new Set(["api", "migrate", "web", "worker"]);

const readJSON = (file) => {
  try {
    return JSON.parse(readFileSync(file, "utf8"));
  } catch {
    throw new Error(`Invalid Railway readiness evidence: ${basename(file)}`);
  }
};

const collectionSize = (value) => {
  if (Array.isArray(value)) return value.length;
  if (Array.isArray(value?.edges)) return value.edges.length;
  if (Array.isArray(value?.domains)) return value.domains.length;
  if (Array.isArray(value?.domains?.edges)) return value.domains.edges.length;
  if (Array.isArray(value?.tcpProxies)) return value.tcpProxies.length;
  if (Array.isArray(value?.proxies)) return value.proxies.length;
  if (Array.isArray(value?.tcpProxies?.edges)) return value.tcpProxies.edges.length;
  return -1;
};

export const verifyTopology = (status, services, buckets, environment, expectedEnvironment) => {
  const actualServices = Array.isArray(services)
    ? services.map((service) => service.name).sort()
    : [];
  if (status.name !== "Relantern" || typeof status.id !== "string") {
    throw new Error("Railway readiness is linked to an unexpected project");
  }
  const environments = status.environments?.edges ?? [];
  if (
    environments.length !== 1 ||
    environments[0]?.node?.name !== expectedEnvironment ||
    environments[0]?.node?.canAccess !== true
  ) {
    throw new Error(`Railway readiness did not resolve only ${expectedEnvironment}`);
  }
  if (JSON.stringify(actualServices) !== JSON.stringify(serviceNames)) {
    throw new Error(`Railway ${expectedEnvironment} has unexpected services`);
  }
  const bucketNames = Array.isArray(buckets) ? buckets.map((bucket) => bucket.name).sort() : [];
  if (bucketNames.length !== 1 || bucketNames[0] !== "bucket") {
    throw new Error(`Railway ${expectedEnvironment} must contain only the private bucket resource`);
  }
  if (environment.privateNetworkDisabled !== false) {
    throw new Error(`Railway ${expectedEnvironment} private networking is not enabled`);
  }
};

export const verifyDeployments = (evidence, expectedSHA) => {
  let webDomain;
  for (const service of serviceNames) {
    const { status, deployments, domains, tcpProxies } = evidence[service] ?? {};
    if (
      status?.name !== service ||
      status.status !== "SUCCESS" ||
      typeof status.deploymentId !== "string" ||
      status.deploymentId.length === 0 ||
      status.stopped !== (service === "migrate")
    ) {
      throw new Error(`Railway ${service} has no successful active deployment`);
    }
    if (!Array.isArray(deployments)) {
      throw new Error(`Railway ${service} deployment list is invalid`);
    }
    const activeDeployment = deployments.find(
      (deployment) => deployment.id === status.deploymentId,
    );
    if (activeDeployment?.status !== "SUCCESS") {
      throw new Error(`Railway ${service} active deployment is not successful`);
    }
    if (gitServices.has(service) && activeDeployment.meta?.commitHash !== expectedSHA) {
      throw new Error(`Railway ${service} is not running the frozen release SHA`);
    }
    const domainCount = collectionSize(domains);
    if (domainCount < 0 || (service === "web" ? domainCount !== 1 : domainCount !== 0)) {
      throw new Error(`Railway ${service} violates the web-only public-domain boundary`);
    }
    if (collectionSize(tcpProxies) !== 0) {
      throw new Error(`Railway ${service} must not expose a public TCP proxy`);
    }
    if (service === "web") {
      webDomain = domains.domains?.[0];
    }
  }
  return parseWebDomain(webDomain);
};

export const parseWebDomain = (domain) => {
  if (typeof domain !== "string") {
    throw new Error("Railway web must have one HTTPS domain");
  }
  let url;
  try {
    url = new URL(domain);
  } catch {
    throw new Error("Railway web must have one HTTPS domain");
  }
  if (
    url.protocol !== "https:" ||
    url.username !== "" ||
    url.password !== "" ||
    url.pathname !== "/" ||
    url.search !== "" ||
    url.hash !== "" ||
    url.hostname === "" ||
    (domain !== url.origin && domain !== `${url.origin}/`)
  ) {
    throw new Error("Railway web must have one HTTPS domain without URL components");
  }
  return url;
};

export const probeWebReadiness = async (webDomain, request = fetch) => {
  const url = new URL("/readyz", parseWebDomain(webDomain.toString()));
  let response;
  try {
    response = await request(url, {
      method: "GET",
      headers: { accept: "application/json" },
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.timeout(5_000),
    });
  } catch {
    throw new Error("Railway web anonymous HTTPS readiness request failed");
  }
  if (response.status !== 200) {
    throw new Error("Railway web anonymous HTTPS readiness did not return HTTP 200");
  }
  if (!response.headers.get("content-type")?.toLowerCase().startsWith("application/json")) {
    throw new Error("Railway web anonymous HTTPS readiness returned an invalid content type");
  }
  let payload;
  try {
    payload = await response.json();
  } catch {
    throw new Error("Railway web anonymous HTTPS readiness returned invalid JSON");
  }
  if (
    typeof payload !== "object" ||
    payload === null ||
    Array.isArray(payload) ||
    payload.service !== "web" ||
    payload.status !== "ready"
  ) {
    throw new Error("Railway web anonymous HTTPS readiness returned a mismatched response");
  }
};

const main = async () => {
  const [, , phase, evidenceDirectory, expected] = process.argv;
  if (phase === "topology") {
    verifyTopology(
      readJSON(join(evidenceDirectory, "status.json")),
      readJSON(join(evidenceDirectory, "services.json")),
      readJSON(join(evidenceDirectory, "buckets.json")),
      readJSON(join(evidenceDirectory, "environment.json")),
      expected,
    );
    process.stdout.write(`Railway ${expected} topology passed.\n`);
    return;
  }
  if (phase === "deployments") {
    const evidence = Object.fromEntries(
      serviceNames.map((service) => [
        service,
        {
          status: readJSON(join(evidenceDirectory, `${service}-status.json`)),
          deployments: readJSON(join(evidenceDirectory, `${service}-deployments.json`)),
          domains: readJSON(join(evidenceDirectory, `${service}-domains.json`)),
          tcpProxies: readJSON(join(evidenceDirectory, `${service}-tcp.json`)),
        },
      ]),
    );
    const webDomain = verifyDeployments(evidence, expected);
    await probeWebReadiness(webDomain);
    process.stdout.write(
      "Railway deployment SHA, exposure, and live web readiness checks passed.\n",
    );
    return;
  }
  throw new Error("Invalid Railway readiness audit phase");
};

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : "Railway readiness audit failed"}\n`,
    );
    process.exitCode = 1;
  });
}
