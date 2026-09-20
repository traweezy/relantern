import { createHash } from "node:crypto";
import { parse } from "parse5";

// Parse as a browser does: regexes miss valid end-tag whitespace and quoted >.
export const inlineScriptHashes = (html) => {
  const hashes = new Set();
  const pending = [parse(html)];
  while (pending.length > 0) {
    const node = pending.pop();
    if (node.tagName === "script" && !node.attrs.some((attribute) => attribute.name === "src")) {
      const source = node.childNodes.map((child) => child.value ?? "").join("");
      hashes.add(`'sha256-${createHash("sha256").update(source).digest("base64")}'`);
    }
    pending.push(...(node.childNodes ?? []));
  }
  return [...hashes].sort();
};
