import type { ReactNode } from "react";
import { WorkspaceShell } from "@/features/intelligence/workspace-shell";
import { requireOwnerSession } from "@/server/auth/session";

type PrivateLayoutProps = Readonly<{
  children: ReactNode;
}>;

const PrivateLayout = async ({ children }: PrivateLayoutProps) => {
  const owner = await requireOwnerSession();
  return <WorkspaceShell owner={owner}>{children}</WorkspaceShell>;
};

export default PrivateLayout;
