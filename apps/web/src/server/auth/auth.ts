import "server-only";

import type { BetterAuthOptions } from "better-auth";
import { betterAuth } from "better-auth";
import { nextCookies } from "better-auth/next-js";
import { genericOAuth } from "better-auth/plugins";
import { getAuthConfiguration } from "./auth-config";
import { getAuthDatabase } from "./database";
import { isAllowedOwnerSource, mapOwnerProfile } from "./owner-policy";

const ownerRejection = {
  error: "owner_access_required",
  errorDescription: "This private application is limited to its configured owner.",
} as const;

const createAuth = () => {
  const configuration = getAuthConfiguration();
  const database = getAuthDatabase(configuration);
  const mapProfile = (profile: unknown) => mapOwnerProfile(profile, configuration.ownerTimezone);
  const providerPlugins =
    configuration.providerMode === "fixture"
      ? [
          genericOAuth({
            config: [
              {
                accountIssuer: "https://github.com",
                authorizationUrl: configuration.oauthAuthorizationURL,
                clientId: configuration.oauthClientID,
                clientSecret: configuration.oauthClientSecret,
                mapProfileToUser: mapProfile,
                name: "GitHub",
                pkce: true,
                providerId: "github",
                scopes: ["read:user"],
                tokenUrl: configuration.oauthTokenURL,
                userInfoUrl: configuration.oauthUserInfoURL,
              },
            ],
          }),
        ]
      : [];

  const options = {
    account: {
      accountLinking: {
        allowDifferentEmails: false,
        allowUnlinkingAll: false,
        enabled: true,
        requireLocalEmailVerified: true,
        trustedProviders: ["github"],
        updateUserInfoOnLink: false,
      },
      encryptOAuthTokens: true,
      fields: {
        accessToken: "access_token",
        accessTokenExpiresAt: "access_token_expires_at",
        accountId: "account_id",
        createdAt: "created_at",
        idToken: "id_token",
        issuer: "issuer",
        password: "password",
        providerId: "provider_id",
        refreshToken: "refresh_token",
        refreshTokenExpiresAt: "refresh_token_expires_at",
        scope: "scope",
        updatedAt: "updated_at",
        userId: "user_id",
      },
      modelName: "auth_accounts",
      updateAccountOnSignIn: true,
    },
    advanced: {
      cookiePrefix: "relantern",
      database: { generateId: "uuid" },
      defaultCookieAttributes: {
        httpOnly: true,
        path: "/",
        sameSite: "lax",
        secure: configuration.secureCookies,
      },
      disableCSRFCheck: false,
      disableOriginCheck: false,
      useSecureCookies: configuration.secureCookies,
    },
    appName: "Relantern",
    baseURL: configuration.baseURL,
    database,
    databaseHooks: {
      session: {
        create: {
          before: async (session) => {
            const result = await database.query<{ github_user_id: string }>(
              "select github_user_id::text from app.users where id = $1::uuid",
              [session.userId],
            );
            return result.rows[0]?.github_user_id === configuration.allowedGitHubUserID;
          },
        },
      },
      user: {
        update: {
          before: async () => false,
        },
      },
    },
    emailAndPassword: { enabled: false },
    onAPIError: {
      errorURL: "/login",
      onError: () => {
        console.error(JSON.stringify({ event: "auth_api_error", severity: "warn" }));
      },
    },
    plugins: [...providerPlugins, nextCookies()],
    rateLimit: {
      customRules: {
        "/sign-in/social": { max: 5, window: 60 },
      },
      enabled: true,
      fields: {
        count: "count",
        key: "key",
        lastRequest: "last_request",
      },
      max: 100,
      modelName: "auth_rate_limits",
      storage: "database",
      window: 60,
    },
    secret: configuration.secret,
    session: {
      cookieCache: { enabled: false },
      expiresIn: configuration.sessionMaxAgeSeconds,
      fields: {
        createdAt: "created_at",
        expiresAt: "expires_at",
        ipAddress: "ip_address",
        token: "token",
        updatedAt: "updated_at",
        userAgent: "user_agent",
        userId: "user_id",
      },
      freshAge: 900,
      modelName: "auth_sessions",
      updateAge: Math.min(86_400, Math.floor(configuration.sessionMaxAgeSeconds / 4)),
    },
    socialProviders:
      configuration.providerMode === "github"
        ? {
            github: {
              clientId: configuration.oauthClientID,
              clientSecret: configuration.oauthClientSecret,
              mapProfileToUser: mapProfile,
              overrideUserInfoOnSignIn: false,
            },
          }
        : undefined,
    trustedOrigins: [configuration.baseURL],
    user: {
      additionalFields: {
        githubUserId: {
          bigint: true,
          fieldName: "github_user_id",
          input: true,
          required: true,
          returned: false,
          type: "number",
        },
        login: {
          fieldName: "login",
          input: true,
          required: true,
          returned: true,
          type: "string",
        },
        timezone: {
          fieldName: "timezone",
          input: true,
          required: true,
          returned: true,
          type: "string",
        },
      },
      fields: {
        createdAt: "created_at",
        email: "email",
        emailVerified: "email_verified",
        image: "image_url",
        name: "display_name",
        updatedAt: "updated_at",
      },
      modelName: "users",
      validateUserInfo: ({ source }) => {
        if (!isAllowedOwnerSource(source, configuration.allowedGitHubUserID)) {
          return ownerRejection;
        }
      },
    },
    verification: {
      fields: {
        createdAt: "created_at",
        expiresAt: "expires_at",
        identifier: "identifier",
        updatedAt: "updated_at",
        value: "value",
      },
      modelName: "auth_verifications",
      storeIdentifier: "hashed",
    },
  } satisfies BetterAuthOptions;

  return betterAuth(options);
};

type AuthInstance = ReturnType<typeof createAuth>;

let authInstance: AuthInstance | undefined;

export const getAuth = (): AuthInstance => {
  authInstance ??= createAuth();
  return authInstance;
};
