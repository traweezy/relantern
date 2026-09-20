import { execFileSync } from "node:child_process";
import { createHash, timingSafeEqual } from "node:crypto";
import { chmod, mkdir, mkdtemp, readFile, rename, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { isAbsolute, join } from "node:path";

const version = "11.24.0";
const integrity =
  "vSfjRel23LC+C3oSKCF7BJqBfiGx81XJDb59xGZxiVqLwebQbCRVRQXqk+oLRfSJon7Bv7yN5qlln8oPFvoAAA==";
const archiveUrl = `https://registry.npmjs.org/pnpm/-/pnpm-${version}.tgz`;
const maximumArchiveBytes = 20 * 1024 * 1024;
const installRoot = process.argv[2];

if (process.version !== "v26.8.1") {
  throw new Error(`Node v26.8.1 is required; found ${process.version}`);
}
if (!installRoot || !isAbsolute(installRoot) || installRoot === "/") {
  throw new Error("Provide an absolute pnpm installation directory");
}

const response = await fetch(archiveUrl, {
  redirect: "error",
  signal: AbortSignal.timeout(30_000),
});
if (!response.ok || !response.body) {
  throw new Error(`pnpm archive request failed with HTTP ${response.status}`);
}

const chunks = [];
let archiveBytes = 0;
for await (const chunk of response.body) {
  archiveBytes += chunk.byteLength;
  if (archiveBytes > maximumArchiveBytes) {
    throw new Error("pnpm archive exceeds the expected size limit");
  }
  chunks.push(Buffer.from(chunk));
}
const archive = Buffer.concat(chunks, archiveBytes);
const actualDigest = createHash("sha512").update(archive).digest();
const expectedDigest = Buffer.from(integrity, "base64");
if (
  actualDigest.length !== expectedDigest.length ||
  !timingSafeEqual(actualDigest, expectedDigest)
) {
  throw new Error("pnpm archive SHA-512 does not match the pinned integrity record");
}

const staging = await mkdtemp(join(tmpdir(), "relantern-pnpm-"));
try {
  const archivePath = join(staging, "pnpm.tgz");
  await writeFile(archivePath, archive, { mode: 0o600 });
  execFileSync("tar", ["-xzf", archivePath, "-C", staging], { stdio: "inherit" });
  const packageRoot = join(staging, "package");
  const metadata = JSON.parse(await readFile(join(packageRoot, "package.json"), "utf8"));
  if (metadata.name !== "pnpm" || metadata.version !== version) {
    throw new Error("pnpm archive metadata does not match the pinned version");
  }
  await chmod(join(packageRoot, "bin/pnpm.cjs"), 0o755);
  await mkdir(installRoot, { recursive: true });
  await rename(packageRoot, join(installRoot, "package"));
  await symlink("package/bin/pnpm.cjs", join(installRoot, "pnpm"));
} finally {
  await rm(staging, { recursive: true, force: true });
}

process.stdout.write(`Installed checksum-verified pnpm ${version}\n`);
