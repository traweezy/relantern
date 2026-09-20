const AlertsLoading = () => (
  <>
    <header className="intelligence-header alerts-header">
      <div>
        <p className="eyebrow">Alert history · Owner only</p>
        <h1>Confirmed dependency alerts.</h1>
      </div>
    </header>
    <section aria-busy="true" aria-label="Alert history" className="alert-history">
      <p role="status">Loading alert history…</p>
      <div aria-hidden="true" className="alert-history-list">
        <div className="skeleton-panel alert-history-skeleton" />
        <div className="skeleton-panel alert-history-skeleton" />
        <div className="skeleton-panel alert-history-skeleton" />
        <div className="skeleton-panel alert-history-skeleton" />
      </div>
    </section>
  </>
);

export default AlertsLoading;
