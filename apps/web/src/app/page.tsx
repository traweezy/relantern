import { memo } from "react";

const HomePage = memo(() => (
  <main className="mx-auto flex min-h-screen max-w-4xl items-center px-6 py-16">
    <section
      aria-labelledby="foundation-title"
      className="w-full rounded-2xl border border-border bg-surface-raised/90 p-8 shadow-2xl shadow-black/20"
    >
      <p className="font-mono text-sm uppercase tracking-[0.18em] text-accent">PR 0 foundation</p>
      <h1 id="foundation-title" className="mt-3 text-4xl font-semibold tracking-tight">
        Relantern
      </h1>
      <p className="mt-4 max-w-2xl text-lg leading-8 text-ink-muted">
        The reproducible local platform is ready for review. Source ingestion and live providers
        remain disabled until the PR 0 evidence gate passes.
      </p>
      <dl className="mt-8 grid gap-4 font-mono text-sm sm:grid-cols-3">
        <div className="rounded-lg border border-border p-4">
          <dt className="text-ink-muted">Runtime</dt>
          <dd className="mt-1 text-ink">Node 24 LTS</dd>
        </div>
        <div className="rounded-lg border border-border p-4">
          <dt className="text-ink-muted">Backend</dt>
          <dd className="mt-1 text-ink">Go 1.27</dd>
        </div>
        <div className="rounded-lg border border-border p-4">
          <dt className="text-ink-muted">Database</dt>
          <dd className="mt-1 text-ink">PostgreSQL 18.6</dd>
        </div>
      </dl>
    </section>
  </main>
));

HomePage.displayName = "HomePage";

export default HomePage;
