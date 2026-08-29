import type { Metadata } from "next";
import Link from "next/link";
import type { ReactNode } from "react";
import { getDemoSnapshot } from "@/features/demo/demo-adapter";

export const metadata: Metadata = {
  description:
    "An isolated synthetic demonstration of Relantern's evidence-first developer intelligence workflow.",
  robots: { follow: false, index: false },
  title: "Demonstration | Relantern",
};

type DemoLayoutProps = Readonly<{
  children: ReactNode;
}>;

const DemoLayout = ({ children }: DemoLayoutProps) => {
  const snapshot = getDemoSnapshot();
  return (
    <div className="demo-frame">
      <a className="skip-link" href="#demo-main">
        Skip to demonstration
      </a>
      <aside className="demo-banner" role="note">
        <span>Demonstration · illustrative data</span>
        <a href={snapshot.methodologyURL} rel="noreferrer" target="_blank">
          Snapshot methodology <span aria-hidden="true">↗</span>
        </a>
      </aside>
      <header className="demo-header">
        <Link aria-label="Relantern demonstration home" className="brand-lockup" href="/demo">
          <span aria-hidden="true" className="brand-mark">
            R
          </span>
          <span>
            <strong>Relantern</strong>
            <small>Evidence-first intelligence</small>
          </span>
        </Link>
        <nav aria-label="Demonstration utility">
          <a href="https://github.com/traweezy" rel="noreferrer" target="_blank">
            Contact
          </a>
          <a href="/login">Owner sign in</a>
        </nav>
      </header>
      <main id="demo-main">{children}</main>
    </div>
  );
};

export default DemoLayout;
