import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import { inlineScriptHashes } from "./demo-csp.mjs";

const hash = (source) => `'sha256-${createHash("sha256").update(source).digest("base64")}'`;

test("hashes valid script tags with end-tag whitespace and quoted delimiters", () => {
  const html =
    '<SCRIPT data-note="a > b">window.first = 1;</SCRIPT >' +
    "<script>window.second = 2;</script ignored>";
  assert.deepEqual(
    inlineScriptHashes(html),
    [hash("window.first = 1;"), hash("window.second = 2;")].sort(),
  );
});

test("ignores external scripts and inert markup", () => {
  const html =
    '<script src="/app.js">ignored</script>' +
    "<!-- <script>comment</script> -->" +
    "<noscript><script>fallback</script></noscript>" +
    "<template><script>template</script></template>";
  assert.deepEqual(inlineScriptHashes(html), []);
});

test("hashes browser-normalized raw text once without decoding entities", () => {
  const html =
    '<script>const text = "&amp;";\r\n</script>' + '<script>const text = "&amp;";\n</script>';
  assert.deepEqual(inlineScriptHashes(html), [hash('const text = "&amp;";\n')]);
});
