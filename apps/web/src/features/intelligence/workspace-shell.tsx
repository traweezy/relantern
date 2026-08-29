import type { ReactNode } from "react";
import { memo } from "react";
import { SignOutButton } from "@/components/auth/sign-out-button";
import type { OwnerSession } from "@/server/auth/session";
import { WorkspaceNavigation } from "./workspace-navigation";

type WorkspaceShellProps = Readonly<{
  children: ReactNode;
  owner: OwnerSession;
}>;

const WorkspaceShellComponent = ({ children, owner }: WorkspaceShellProps) => (
  <div className="app-frame">
    <a className="skip-link" href="#workspace-main">
      Skip to workspace
    </a>
    <aside aria-label="Primary navigation" className="app-sidebar">
      <div className="brand-lockup">
        <span aria-hidden="true" className="brand-mark">
          R
        </span>
        <div>
          <p className="brand-name">Relantern</p>
          <p className="brand-caption">Private intelligence</p>
        </div>
      </div>
      <nav aria-label="Workspace">
        <WorkspaceNavigation />
      </nav>
      <div className="sidebar-account">
        <div aria-hidden="true" className="owner-avatar">
          {owner.displayName.slice(0, 1).toUpperCase()}
        </div>
        <div className="owner-copy">
          <p>{owner.displayName}</p>
          <p>@{owner.login}</p>
        </div>
        <SignOutButton />
      </div>
    </aside>
    <div className="workspace-column">
      <header className="workspace-topbar">
        <div>
          <p className="topbar-label">Evidence window</p>
          <p className="topbar-value">Continuous · {owner.timezone}</p>
        </div>
        <span className="security-chip">
          <span aria-hidden="true" className="security-dot" />
          Owner verified
        </span>
      </header>
      <main className="workspace-main intelligence-main" id="workspace-main">
        {children}
      </main>
    </div>
    <nav aria-label="Mobile workspace" className="mobile-navigation">
      <WorkspaceNavigation compact />
    </nav>
  </div>
);

export const WorkspaceShell = memo<WorkspaceShellProps>(WorkspaceShellComponent);
WorkspaceShell.displayName = "WorkspaceShell";
