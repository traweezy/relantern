import { fileURLToPath } from "node:url";
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  agentRules: false,
  output: "export",
  poweredByHeader: false,
  turbopack: { root: fileURLToPath(new URL("../../..", import.meta.url)) },
};

export default nextConfig;
