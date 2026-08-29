import "server-only";

import { Pool } from "pg";
import type { AuthConfiguration } from "./auth-config";

let pool: Pool | undefined;

export const getAuthDatabase = (configuration: AuthConfiguration): Pool => {
  pool ??= new Pool({
    application_name: "relantern-web-auth",
    connectionString: configuration.databaseURL,
    connectionTimeoutMillis: 5_000,
    idleTimeoutMillis: 30_000,
    max: 10,
    options: "-c search_path=app,public -c statement_timeout=10000",
  });
  pool.on("error", () => {
    console.error(JSON.stringify({ event: "auth_database_pool_error", severity: "error" }));
  });
  return pool;
};
