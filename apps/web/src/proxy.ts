import { getSessionCookie } from "better-auth/cookies";
import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";
import { isProtectedAPIRoute, isPublicRoute } from "@/security/route-policy";

const createNonce = (): string => btoa(crypto.randomUUID());

const createContentSecurityPolicy = (nonce: string): string =>
  [
    "default-src 'self'",
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'`,
    `style-src 'self' 'nonce-${nonce}'`,
    "img-src 'self' data:",
    "font-src 'self'",
    "connect-src 'self'",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'self'",
    "frame-ancestors 'none'",
    "upgrade-insecure-requests",
  ].join("; ");

const withSecurityHeaders = (
  response: NextResponse,
  contentSecurityPolicy: string,
): NextResponse => {
  response.headers.set("Content-Security-Policy", contentSecurityPolicy);
  return response;
};

export const proxy = (request: NextRequest): NextResponse => {
  const nonce = createNonce();
  const contentSecurityPolicy = createContentSecurityPolicy(nonce);
  const pathname = request.nextUrl.pathname;
  const hasSessionCookie = getSessionCookie(request, { cookiePrefix: "relantern" }) !== null;

  if (!isPublicRoute(pathname) && !hasSessionCookie) {
    if (isProtectedAPIRoute(pathname)) {
      return withSecurityHeaders(
        NextResponse.json(
          {
            detail: "An authenticated owner session is required.",
            status: 401,
            title: "Unauthorized",
            type: "about:blank",
          },
          { status: 401 },
        ),
        contentSecurityPolicy,
      );
    }
    return withSecurityHeaders(
      NextResponse.redirect(new URL("/login", request.url)),
      contentSecurityPolicy,
    );
  }

  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("Content-Security-Policy", contentSecurityPolicy);
  return withSecurityHeaders(
    NextResponse.next({ request: { headers: requestHeaders } }),
    contentSecurityPolicy,
  );
};

export const config = {
  matcher: [
    {
      source: "/((?!_next/static|_next/image|favicon.ico).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
};
