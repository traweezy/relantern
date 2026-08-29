import { isPublicDemoPath } from "../features/demo/demo-route";

const publicExactRoutes = new Set(["/favicon.ico", "/healthz", "/login", "/robots.txt"]);

export const isPublicRoute = (pathname: string): boolean =>
  publicExactRoutes.has(pathname) ||
  isPublicDemoPath(pathname) ||
  pathname.startsWith("/api/auth/") ||
  pathname === "/api/webhooks/openai";

export const isProtectedAPIRoute = (pathname: string): boolean =>
  pathname.startsWith("/api/") && !isPublicRoute(pathname);
