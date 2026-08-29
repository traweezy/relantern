import { memo } from "react";

export const metadata = {
  robots: "noindex,nofollow",
  title: "Relantern demo — awaiting fixture approval",
};

const DemoPage = memo(() => (
  <main className="mx-auto flex min-h-screen max-w-4xl items-center px-6 py-16">
    <section
      aria-labelledby="demo-title"
      className="w-full rounded-2xl border border-border bg-surface-raised/90 p-8"
    >
      <p className="font-mono text-sm uppercase tracking-[0.18em] text-accent">
        Safe demo boundary
      </p>
      <h1 id="demo-title" className="mt-3 text-4xl font-semibold tracking-tight">
        Synthetic fixtures are awaiting review
      </h1>
      <p className="mt-4 max-w-2xl text-lg leading-8 text-ink-muted">
        This public route is isolated from authentication, databases, private APIs, providers, and
        owner data. The guided demo will be added only after its static fixture snapshot is
        approved.
      </p>
    </section>
  </main>
));

DemoPage.displayName = "DemoPage";

export default DemoPage;
