import "server-only";

import type { Route } from "next";
import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { connection } from "next/server";
import { cache } from "react";
import { getAuth } from "./auth";
import { getAuthConfiguration } from "./auth-config";
import { getAuthDatabase } from "./database";

export type OwnerSession = Readonly<{
  displayName: string;
  expiresAt: Date;
  login: string;
  timezone: string;
  userID: string;
}>;

export const getOwnerSession = cache(async (): Promise<OwnerSession | null> => {
  await connection();
  const configuration = getAuthConfiguration();
  const session = await getAuth().api.getSession({ headers: await headers() });
  if (session === null) {
    return null;
  }
  const result = await getAuthDatabase(configuration).query<{
    display_name: string;
    github_user_id: string;
    login: string;
    timezone: string;
  }>(
    `select github_user_id::text, login, display_name, timezone
     from app.users
     where id = $1::uuid`,
    [session.user.id],
  );
  const owner = result.rows[0];
  if (owner === undefined || owner.github_user_id !== configuration.allowedGitHubUserID) {
    return null;
  }
  return {
    displayName: owner.display_name,
    expiresAt: session.session.expiresAt,
    login: owner.login,
    timezone: owner.timezone,
    userID: session.user.id,
  };
});

export const requireOwnerSession = async (): Promise<OwnerSession> => {
  const session = await getOwnerSession();
  if (session === null) {
    redirect("/login" as Route);
  }
  return session;
};
