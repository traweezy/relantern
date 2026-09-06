import Link from "next/link";

const MethodologyPage = () => (
  <div className="demo-story-page">
    <article className="story-reader">
      <header className="story-reader-header">
        <p className="eyebrow">About this demonstration</p>
        <h1>Real interface. Illustrative data.</h1>
        <p className="story-reader-summary">
          Explore the reading workflow, then follow the evidence behind each claim.
        </p>
        <Link className="primary-button story-primary-source" href="/demo">
          Return to the demo →
        </Link>
      </header>
      <section className="reader-section reader-brief">
        <h2>A fixed snapshot</h2>
        <p>
          Stories, source counts, costs, and radar decisions are hand-authored examples dated 14
          October 2025. They demonstrate the workflow and are not a current news feed or measured
          production results. Source links point to the original technical documentation.
        </p>
      </section>
      <section className="reader-section reader-brief">
        <h2>Explore without an account</h2>
        <p>
          Try the guided tour, switch between Today, Live, Radar, and Sources + Ops, and open a
          story to inspect its claims and evidence. Reading actions affect only this demo in your
          browser. Reset demo restores the initial state.
        </p>
      </section>
      <section className="reader-section reader-brief">
        <h2>An isolated view of work in progress</h2>
        <p>
          This deployment serves only static demo pages and assets. It has no private reading
          history, sign-in, database, AI provider, or delivery connection. The wider Relantern
          application remains under development.
        </p>
      </section>
    </article>
  </div>
);

export default MethodologyPage;
