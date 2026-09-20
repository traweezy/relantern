import assert from "node:assert/strict";
import test from "node:test";
import {
  parseWebDomain,
  probeWebReadiness,
  verifyDeployments,
  verifyTopology,
} from "./railway-readiness.mjs";

const frozenSHA = "b".repeat(40);
const serviceNames = ["api", "migrate", "postgres", "web", "worker"];

const makeEvidence = () =>
  Object.fromEntries(
    serviceNames.map((service) => [
      service,
      {
        status: {
          name: service,
          status: "SUCCESS",
          deploymentId: `${service}-active`,
          stopped: service === "migrate",
        },
        deployments: [
          { id: `${service}-old`, status: "REMOVED", meta: { commitHash: "a".repeat(40) } },
          { id: `${service}-active`, status: "SUCCESS", meta: { commitHash: frozenSHA } },
        ],
        domains: { domains: service === "web" ? ["https://web.example.test"] : [] },
        tcpProxies: { proxies: [] },
      },
    ]),
  );

const makeTopology = () => ({
  status: {
    name: "Relantern",
    id: "project-1",
    environments: { edges: [{ node: { name: "staging", canAccess: true } }] },
  },
  services: serviceNames.map((name) => ({ name })),
  buckets: [{ name: "bucket" }],
  environment: { privateNetworkDisabled: false },
});

test("provider topology fixture resolves only the requested private environment", () => {
  const { status, services, buckets, environment } = makeTopology();
  assert.doesNotThrow(() => verifyTopology(status, services, buckets, environment, "staging"));
  assert.throws(
    () => verifyTopology(status, services, buckets, environment, "production"),
    /did not resolve only production/,
  );
});

test("provider deployment fixture joins the active deployment ID and frozen SHA", () => {
  const evidence = makeEvidence();
  evidence.postgres.deployments[1].meta = {};
  evidence.api.deployments[0].status = "SUCCESS";
  assert.equal(verifyDeployments(evidence, frozenSHA).origin, "https://web.example.test");

  evidence.api.status.deploymentId = "api-old";
  assert.throws(() => verifyDeployments(evidence, frozenSHA), /frozen release SHA/);

  evidence.api.status.deploymentId = "api-active";
  evidence.api.deployments[1].meta.commitHash = "a".repeat(40);
  assert.throws(() => verifyDeployments(evidence, frozenSHA), /frozen release SHA/);
});

test("provider fixture handles completed migration and rejects stopped worker", () => {
  const evidence = makeEvidence();
  assert.doesNotThrow(() => verifyDeployments(evidence, frozenSHA));
  evidence.worker.status.stopped = true;
  assert.throws(() => verifyDeployments(evidence, frozenSHA), /worker has no successful/);
});

test("provider fixture rejects extra domains and public TCP proxies", () => {
  const evidence = makeEvidence();
  evidence.web.domains.domains.push("https://other.example.test");
  assert.throws(() => verifyDeployments(evidence, frozenSHA), /web-only public-domain boundary/);
  evidence.web.domains.domains.pop();
  evidence.web.domains.domains.pop();
  assert.throws(() => verifyDeployments(evidence, frozenSHA), /web-only public-domain boundary/);
  evidence.web.domains.domains.push("https://web.example.test");
  evidence.api.tcpProxies.proxies.push({});
  assert.throws(() => verifyDeployments(evidence, frozenSHA), /public TCP proxy/);
});

test("web domain permits only a sole HTTPS origin", () => {
  assert.equal(parseWebDomain("https://web.example.test/").origin, "https://web.example.test");
  for (const invalid of [
    "http://web.example.test",
    "https://user:pass@web.example.test",
    "https://web.example.test/path",
    "https://web.example.test?token=abc",
    "https://web.example.test/#fragment",
  ]) {
    assert.throws(() => parseWebDomain(invalid), /HTTPS domain/);
  }
});

test("anonymous web probe accepts only an HTTP 200 ready JSON response", async () => {
  let calls = 0;
  const request = async (url, options) => {
    calls += 1;
    assert.equal(url.toString(), "https://web.example.test/readyz");
    assert.equal(options.method, "GET");
    assert.equal(options.redirect, "error");
    assert.equal(options.cache, "no-store");
    assert.deepEqual(options.headers, { accept: "application/json" });
    assert.ok(options.signal instanceof AbortSignal);
    return Response.json({ service: "web", status: "ready" });
  };
  await probeWebReadiness("https://web.example.test", request);
  assert.equal(calls, 1);
});

test("anonymous web probe rejects HTTP 503, mismatched JSON, and redirects", async () => {
  const domain = "https://web.example.test";
  const responses = [
    [() => Response.json({ status: "unavailable" }, { status: 503 }), /HTTP 200/],
    [() => Response.json({ service: "api", status: "ready" }), /mismatched response/],
    [() => Response.redirect("https://other.example.test/readyz", 302), /HTTP 200/],
  ];
  for (const [response, message] of responses) {
    await assert.rejects(() => probeWebReadiness(domain, async () => response()), message);
  }
});
