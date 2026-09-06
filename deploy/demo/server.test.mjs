import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, test } from "node:test";
import { createDemoServer, loadAssets } from "./server.mjs";

const policy =
  "default-src 'self'; script-src 'self' 'sha256-example'; connect-src 'self'; form-action 'none'";
let directory;
let manifest;
let server;
let origin;
const events = [];

before(async () => {
  directory = await mkdtemp(join(tmpdir(), "synthetic-demo-test-"));
  const content = "<!doctype html><title>Synthetic demo</title>";
  await writeFile(join(directory, "index.html"), content);
  await writeFile(join(directory, "demo.html"), content);
  manifest = {
    files: ["index.html", "demo.html"].map((path) => ({
      path,
      sha256: createHash("sha256").update(content).digest("hex"),
    })),
  };
  server = createDemoServer(await loadAssets(directory, manifest), policy, (entry) =>
    events.push(entry),
  );
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  origin = `http://127.0.0.1:${server.address().port}`;
});

after(async () => {
  await new Promise((resolve) => server.close(resolve));
  await rm(directory, { recursive: true, force: true });
});

test("serves only listed demo assets with security headers and conditional caching", async () => {
  const response = await fetch(`${origin}/demo?ignored=value`);
  assert.equal(response.status, 200);
  assert.match(await response.text(), /Synthetic demo/);
  assert.equal(response.headers.get("content-security-policy"), policy);
  assert.equal(response.headers.get("x-robots-tag"), "noindex, nofollow, noarchive");
  assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  assert.equal(response.headers.get("vary"), "Accept-Encoding");
  const cached = await fetch(`${origin}/demo`, {
    headers: { "If-None-Match": response.headers.get("etag") },
  });
  assert.equal(cached.status, 304);
  const head = await fetch(`${origin}/`, { method: "HEAD" });
  assert.equal(head.status, 200);
  assert.equal(await head.text(), "");
});

test("does not serve private routes, deployment metadata, directories, or write methods", async () => {
  for (const path of [
    "/dashboard",
    "/login",
    "/api/auth",
    "/api/v1/accounts",
    "/artifact.json",
    "/csp.txt",
    "/server.mjs",
    "/.env",
    "/_next/",
    "/%2e%2e/artifact.json",
  ]) {
    assert.equal((await fetch(`${origin}${path}`)).status, 404, path);
  }
  const response = await fetch(`${origin}/demo`, { method: "POST", body: "ignored" });
  assert.equal(response.status, 405);
  assert.equal(response.headers.get("allow"), "GET, HEAD");
});

test("health checks work without a provider and logs omit query strings and client details", async () => {
  const response = await fetch(`${origin}/healthz?private=test-value`);
  assert.equal(response.status, 200);
  assert.equal(await response.text(), "ok");
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert(events.some((event) => event.route === "health"));
  assert(!JSON.stringify(events).includes("test-value"));
  assert(!JSON.stringify(events).includes("127.0.0.1"));
});

test("rejects corrupt assets and manifest paths outside the public directory", async () => {
  await assert.rejects(
    loadAssets(directory, { files: [{ path: "../private.txt", sha256: "" }] }),
    /Invalid asset path/,
  );
  await assert.rejects(
    loadAssets(directory, { files: [{ path: "index.html", sha256: "wrong" }] }),
    /checksum mismatch/,
  );
  assert.throws(() => createDemoServer(new Map(), "script-src 'unsafe-inline'"), /CSP/);
  assert.throws(() => createDemoServer(new Map(), `${policy}\nInjected: value`), /CSP/);
});
