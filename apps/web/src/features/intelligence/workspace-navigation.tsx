"use client";

import type { Route } from "next";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { memo, useMemo } from "react";

const navigation = [
  { href: "/", label: "Today", shortLabel: "Today" },
  { href: "/live", label: "Live", shortLabel: "Live" },
  { href: "/radar", label: "Radar", shortLabel: "Radar" },
  { href: "/sources", label: "Sources", shortLabel: "Sources" },
  { href: "/ops", label: "Operations", shortLabel: "Ops" },
] as const;

type NavigationListProps = Readonly<{
  compact?: boolean;
}>;

const NavigationListComponent = ({ compact = false }: NavigationListProps) => {
  const pathname = usePathname();
  const items = useMemo(
    () =>
      navigation.map((item) => ({
        ...item,
        current:
          item.href === "/"
            ? pathname === "/" || pathname.startsWith("/story/")
            : pathname === item.href,
        enabled: item.href === "/" || item.href === "/live",
      })),
    [pathname],
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
