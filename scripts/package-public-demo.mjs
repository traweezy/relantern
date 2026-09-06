import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cp, mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const source = join(repository, "apps/web/demo/out");
const destination = join(repository, ".local/public-demo");
const allowedHTML = new Set([
  "index.html",
  "demo.html",
  "404.html",
  "_not-found.html",
  "methodology.html",
  "demo/story/go-toolchain-security.html",
  "demo/story/next-cache-components.html",
  "demo/story/postgresql-18-observability.html",
  "demo/story/react-actions-transition.html",
]);
const hashes = new Set();
const files = [];

const inspectDirectory = async (directory, prefix = "") => {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const relative = `${prefix}${entry.name}`;
    if (entry.isSymbolicLink()) throw new Error(`Demo export contains a symlink: ${relative}`);
    if (entry.isDirectory()) {
      await inspectDirectory(join(directory, entry.name), `${relative}/`);
      continue;
    }
    if (!/\.(?:html|txt|js|css|json|svg|ico|woff2?|png|webp|jpg|jpeg)$/.test(relative)) {
      throw new Error(`Unexpected demo asset: ${relative}`);
    }
    const content = await readFile(join(directory, entry.name));
    if (relative.endsWith(".html")) {
      if (!allowedHTML.has(relative)) throw new Error(`Private route in demo export: ${relative}`);
      for (const match of content.toString().matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)) {
        if (!/\bsrc\s*=/i.test(match[1])) {
          hashes.add(`'sha256-${createHash("sha256").update(match[2]).digest("base64")}'`);
        }
      }
    }
    files.push({ path: relative, sha256: createHash("sha256").update(content).digest("hex") });
  }
};

await inspectDirectory(source);
for (const path of ["index.html", "demo.html", "404.html"]) {
  if (!files.some((file) => file.path === path)) throw new Error(`Missing demo route: ${path}`);
}
if (hashes.size === 0) throw new Error("Missing inline-script CSP hashes");
await rm(destination, { recursive: true, force: true });
await mkdir(destination, { recursive: true });
await cp(source, join(destination, "site"), { recursive: true });
await writeFile(join(destination, "site/robots.txt"), "User-agent: *\nDisallow: /\n");

const robots = await readFile(join(destination, "site/robots.txt"));
files.push({ path: "robots.txt", sha256: createHash("sha256").update(robots).digest("hex") });

const policy = [
  "default-src 'self'",
  "base-uri 'none'",
  "connect-src 'self'",
  "font-src 'self'",
  "form-action 'none'",
  "frame-ancestors 'none'",
  "img-src 'self' data:",
  "object-src 'none'",
  `script-src 'self' ${[...hashes].sort().join(" ")}`,
  "style-src 'self' 'unsafe-inline'",
].join("; ");
await writeFile(join(destination, "csp.txt"), policy);
await cp(join(repository, "deploy/demo/server.mjs"), join(destination, "server.mjs"));
await cp(join(repository, "deploy/demo/Dockerfile"), join(destination, "Dockerfile"));
await cp(join(repository, "deploy/demo/railway.json"), join(destination, "railway.json"));
await writeFile(
  join(destination, ".dockerignore"),
  "*\n!site/\n!site/**\n!server.mjs\n!artifact.json\n!csp.txt\n",
);
await writeFile(
  join(destination, "artifact.json"),
  `${JSON.stringify(
    {
      project: "relantern",
      kind: "synthetic-static-demo",
      sourceCommit: execFileSync("git", ["rev-parse", "HEAD"], {
        cwd: repository,
        encoding: "utf8",
      }).trim(),
      sourceHasLocalChanges:
        execFileSync("git", ["status", "--porcelain"], { cwd: repository, encoding: "utf8" })
          .length > 0,
      files: files.sort((a, b) => a.path.localeCompare(b.path)),
    },
    null,
    2,
  )}\n`,
);
process.stdout.write(
  `Packaged ${files.length} public assets with ${hashes.size} CSP hashes at ${destination}\n`,
);
