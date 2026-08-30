"use client";

import type { Route } from "next";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { memo, useMemo } from "react";

const desktopNavigation = [
  { href: "/", label: "Today", shortLabel: "Today" },
  { href: "/live", label: "Live", shortLabel: "Live" },
  { href: "/inbox", label: "Inbox", shortLabel: "Inbox" },
  { href: "/later", label: "Read Later", shortLabel: "Later" },
  { href: "/starred", label: "Starred", shortLabel: "Starred" },
  { href: "/snoozed", label: "Snoozed", shortLabel: "Snoozed" },
  { href: "/archive", label: "Archive", shortLabel: "Archive" },
  { href: "/releases", label: "Releases", shortLabel: "Releases" },
  { href: "/radar", label: "Radar", shortLabel: "Radar" },
  { href: "/sources", label: "Sources", shortLabel: "Sources" },
  { href: "/ops", label: "Operations", shortLabel: "Ops" },
  { href: "/settings", label: "Settings", shortLabel: "Settings" },
] as const;

const mobileNavigation = [
  { href: "/", label: "Today", shortLabel: "Today" },
  { href: "/inbox", label: "Inbox", shortLabel: "Inbox" },
  { href: "/later", label: "Read Later", shortLabel: "Later" },
  { href: "/starred", label: "Starred", shortLabel: "Starred" },
  { href: "/search", label: "Search", shortLabel: "Search" },
] as const;

type NavigationListProps = Readonly<{
  compact?: boolean;
}>;

const NavigationListComponent = ({ compact = false }: NavigationListProps) => {
  const pathname = usePathname();
  const items = useMemo(
    () =>
      (compact ? mobileNavigation : desktopNavigation).map((item) => ({
        ...item,
        current:
          item.href === "/"
            ? pathname === "/" || pathname.startsWith("/story/")
            : pathname === item.href,
        enabled: item.href !== "/radar",
      })),
    [compact, pathname],
  );
  return (
    <ul className={compact ? "mobile-nav-list" : "sidebar-nav"}>
      {items.map((item) => (
        <li key={item.href}>
          {item.enabled ? (
            <Link
              aria-current={item.current ? "page" : undefined}
              className={`nav-item${item.current ? " nav-item-current" : ""}`}
              href={item.href as Route}
            >
              <span>{compact ? item.shortLabel : item.label}</span>
              {!compact && <span className="nav-state">{item.current ? "Now" : "Open"}</span>}
            </Link>
          ) : (
            <span aria-disabled="true" className="nav-item nav-item-disabled">
              <span>{compact ? item.shortLabel : item.label}</span>
              {!compact && <span className="nav-state">Soon</span>}
            </span>
          )}
        </li>
      ))}
    </ul>
  );
};

export const WorkspaceNavigation = memo<NavigationListProps>(NavigationListComponent);
WorkspaceNavigation.displayName = "WorkspaceNavigation";
