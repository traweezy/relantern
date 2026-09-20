import type { Metadata } from "next";
import Link from "next/link";
import { memo, type ReactNode } from "react";
import "./globals.css";

export const metadata: Metadata = {
  title: "Relantern · Interactive demo",
  description: "Explore evidence-first developer intelligence with illustrative data.",
  robots: { index: false, follow: false },
};

const DemoLayout = memo<Readonly<{ children: ReactNode }>>(({ children }) => (
  <html lang="en">
    <body>
      <div className="demo-frame">
        <a className="skip-link" href="#demo-main">
          Skip to demonstration
        </a>
        <aside aria-label="Demonstration notice" className="demo-banner">
          <span>Demonstration · illustrative data</span>
          <Link href="/methodology">How this demo works</Link>
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
            <a href="https://www.tyschumacher.me/#projects" rel="noreferrer" target="_blank">
              About the developer <span aria-hidden="true">↗</span>
            </a>
          </nav>
        </header>
        <main id="demo-main">{children}</main>
      </div>
    </body>
  </html>
));

DemoLayout.displayName = "DemoLayout";
export default DemoLayout;
