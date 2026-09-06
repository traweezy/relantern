import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { createServer } from "node:http";
import { dirname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { brotliCompressSync, gzipSync } from "node:zlib";

const mimeTypes = {
  css: "text/css; charset=utf-8",
  html: "text/html; charset=utf-8",
  ico: "image/x-icon",
  jpeg: "image/jpeg",
  jpg: "image/jpeg",
  js: "text/javascript; charset=utf-8",
  json: "application/json; charset=utf-8",
  png: "image/png",
  svg: "image/svg+xml",
  txt: "text/plain; charset=utf-8",
  webp: "image/webp",
  woff: "font/woff",
  woff2: "font/woff2",
};

// Only manifest-listed public assets enter memory. Request URLs never access the filesystem.
export const loadAssets = async (directory, manifest) => {
  const assets = new Map();
  for (const entry of manifest.files) {
    const path = resolve(directory, entry.path);
    if (!path.startsWith(`${resolve(directory)}${sep}`)) throw new Error("Invalid asset path");
    const content = await readFile(path);
    const hash = createHash("sha256").update(content).digest("hex");
    if (hash !== entry.sha256) throw new Error(`Asset checksum mismatch: ${entry.path}`);
    const type = mimeTypes[entry.path.split(".").at(-1)];
    if (!type) throw new Error(`Unsupported asset type: ${entry.path}`);
    assets.set(`/${entry.path}`, {
      content,
      gzip: gzipSync(content),
      br: brotliCompressSync(content),
      etag: `W/"${hash}"`,
      type,
    });
  }
  return assets;
};

export const createDemoServer = (assets, policy, log = () => {}) => {
  if (!policy.includes("script-src 'self' 'sha256-") || /[\r\n]/.test(policy)) {
    throw new Error("A static, hash-based CSP is required");
  }
  const server = createServer((request, response) => {
    const started = performance.now();
    let route = "not-found";
    response.once("finish", () =>
      log({
        event: "demo_request",
        method: request.method,
        route,
        status: response.statusCode,
        durationMs: Math.round(performance.now() - started),
      }),
    );
    const headers = {
      "Content-Security-Policy": policy,
      "Referrer-Policy": "no-referrer",
      "Strict-Transport-Security": "max-age=63072000; includeSubDomains",
      "Permissions-Policy": "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
      "X-Content-Type-Options": "nosniff",
      "X-Frame-Options": "DENY",
      "X-Robots-Tag": "noindex, nofollow, noarchive",
      "Cache-Control": "no-store",
    };
    if (!["GET", "HEAD"].includes(request.method)) {
      response.writeHead(405, { ...headers, Allow: "GET, HEAD" }).end();
      return;
    }
    let pathname;
    try {
      pathname = new URL(request.url, "http://demo.invalid").pathname;
    } catch {
      response.writeHead(400, headers).end();
      return;
    }
    if (pathname === "/healthz") {
      route = "health";
      response.writeHead(200, { ...headers, "Content-Type": "text/plain; charset=utf-8" });
      response.end(request.method === "HEAD" ? undefined : "ok");
      return;
    }
    const key = pathname === "/" ? "/index.html" : pathname.replace(/\/$/, "");
    const asset = assets.get(key) ?? assets.get(`${key}.html`);
    if (!asset) {
      response.writeHead(404, headers).end();
      return;
    }
    route = key;
    headers["Content-Type"] = asset.type;
    headers["Cache-Control"] = key.startsWith("/_next/static/")
      ? "public, max-age=31536000, immutable"
      : "public, max-age=60";
    headers.ETag = asset.etag;
    headers.Vary = "Accept-Encoding";
    if (request.headers["if-none-match"] === asset.etag) {
      response.writeHead(304, headers).end();
      return;
    }
    const encodings = (request.headers["accept-encoding"] ?? "")
      .split(",")
      .map((item) => item.trim());
    const encoding = encodings.includes("br") ? "br" : encodings.includes("gzip") ? "gzip" : null;
    const content = encoding ? asset[encoding] : asset.content;
    if (encoding) headers["Content-Encoding"] = encoding;
    headers["Content-Length"] = String(content.length);
    response.writeHead(200, headers);
    response.end(request.method === "HEAD" ? undefined : content);
  });
  server.headersTimeout = 5_000;
  server.requestTimeout = 10_000;
  server.keepAliveTimeout = 5_000;
  server.maxHeadersCount = 50;
  return server;
};

const start = async () => {
  const directory = dirname(fileURLToPath(import.meta.url));
  const port = Number(process.env.PORT ?? 8080);
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error("Invalid PORT");
  const manifest = JSON.parse(await readFile(resolve(directory, "artifact.json"), "utf8"));
  const assets = await loadAssets(resolve(directory, "site"), manifest);
  const policy = await readFile(resolve(directory, "csp.txt"), "utf8");
  const log = (entry) => process.stdout.write(`${JSON.stringify(entry)}\n`);
  const server = createDemoServer(assets, policy, log);
  server.listen(port, "0.0.0.0", () => log({ event: "demo_ready", assets: assets.size, port }));
  const shutdown = () => {
    server.close(() => process.exit(0));
    setTimeout(() => process.exit(1), 10_000).unref();
  };
  process.once("SIGTERM", shutdown);
  process.once("SIGINT", shutdown);
};

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  start().catch(() => {
    process.stderr.write("Demo startup failed: verify PORT and the static artifact.\n");
    process.exitCode = 1;
  });
}
