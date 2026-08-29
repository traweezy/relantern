import { memo } from "react";

type EmptyStateProps = Readonly<{
  detail: string;
  eyebrow: string;
  title: string;
}>;

const EmptyStateComponent = ({ detail, eyebrow, title }: EmptyStateProps) => (
  <section aria-labelledby="empty-state-title" className="empty-state">
    <span aria-hidden="true" className="empty-state-mark">
      R
    </span>
    <div>
      <p className="eyebrow">{eyebrow}</p>
      <h2 id="empty-state-title">{title}</h2>
      <p>{detail}</p>
    </div>
  </section>
);

export const EmptyState = memo<EmptyStateProps>(EmptyStateComponent);
EmptyState.displayName = "EmptyState";
