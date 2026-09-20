import type { AlertDeliveryStatus, AlertHistoryItem, CriticalAlert } from "@relantern/domain";
import { memo } from "react";

type CriticalAlertCardProps = Readonly<{
  alert: CriticalAlert & Partial<Pick<AlertHistoryItem, "correctionReason" | "correctedAt">>;
  deliveries?: readonly AlertDeliveryStatus[];
  timezone: string;
}>;

const formatDate = (value: string, timezone: string): string =>
  new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone,
  }).format(new Date(value));

const deliveryCopy = (delivery: AlertDeliveryStatus): string => {
  switch (delivery.state) {
    case "pending":
      return "Pending";
    case "sending":
      return "Sending";
    case "sent":
      return "Sent";
    case "failed":
      return delivery.nextAttemptAt === null
        ? "Failed; awaiting retry"
        : "Failed; retry scheduled for";
    case "permanent":
      return "Delivery stopped; check Operations";
    case "suppressed":
      return "Stopped after advisory correction";
  }
};

const correctionMessages = {
  withdrawn: {
    title: "Advisory withdrawn",
    detail:
      "The official advisory was withdrawn. Review the current advisory before acting on this earlier alert.",
  },
  no_longer_published: {
    title: "Advisory no longer published",
    detail:
      "The official advisory is no longer published. Review current upstream security information before acting on this earlier alert.",
  },
  severity_downgraded: {
    title: "Severity downgraded",
    detail:
      "The official advisory no longer classifies this issue as critical. Review the current advisory for applicability and remediation.",
  },
  severity_unconfirmed: {
    title: "Severity no longer confirmed as critical",
    detail:
      "The official advisory now lists severity as unknown. Review the current advisory for applicability and remediation.",
  },
} satisfies Record<
  NonNullable<AlertHistoryItem["correctionReason"]>,
  Readonly<{ title: string; detail: string }>
>;

const CriticalAlertCardComponent = ({ alert, deliveries, timezone }: CriticalAlertCardProps) => {
  const correction =
    alert.correctionReason === undefined || alert.correctionReason === null
      ? null
      : correctionMessages[alert.correctionReason];
  const corrected = correction !== null;
  const reopened = alert.episodeNumber > 1;

  return (
    <article
      className={
        corrected ? "critical-alert-card critical-alert-card--corrected" : "critical-alert-card"
      }
    >
      <div className="critical-alert-card-heading">
        <span className="critical-alert-severity">
          {corrected ? "Corrected alert" : reopened ? "Critical again" : "Critical"} ·{" "}
          {alert.ecosystem}
        </span>
        <time dateTime={alert.alertedAt}>{formatDate(alert.alertedAt, timezone)}</time>
      </div>
      <h3>{corrected ? `Original alert: ${alert.title}` : alert.title}</h3>
      {reopened && !corrected && (
        <p>This is a new alert after the official advisory returned to critical.</p>
      )}
      {corrected && (
        <div className="alert-correction">
          <strong>{correction?.title}</strong>
          <p>{correction?.detail}</p>
          {alert.correctedAt !== undefined && alert.correctedAt !== null && (
            <p>
              Corrected{" "}
              <time dateTime={alert.correctedAt}>{formatDate(alert.correctedAt, timezone)}</time>
            </p>
          )}
        </div>
      )}
      <p>
        <strong>{alert.packageName}</strong> at {alert.currentVersion}{" "}
        {corrected ? "was originally reported within" : "is within"} the affected range{" "}
        {alert.versionRange}.
      </p>
      {alert.patchedVersion !== "" && (
        <p>
          {corrected ? "Originally reported first patched version" : "First patched version"}:{" "}
          {alert.patchedVersion}
        </p>
      )}
      <p className="critical-alert-reason">
        {corrected && "Original alert reason: "}
        {alert.reason}
      </p>
      {deliveries !== undefined && (
        <div className="alert-delivery-status">
          <p>Delivery</p>
          <ul>
            <li>Dashboard: {corrected ? "Correction recorded in history" : "Available"}</li>
            {deliveries.map((delivery) => (
              <li key={delivery.channel}>
                {delivery.channel === "discord" ? "Discord" : "Email"}: {deliveryCopy(delivery)}
                {delivery.state === "sent" && delivery.deliveredAt !== null && (
                  <>
                    {" "}
                    <time dateTime={delivery.deliveredAt}>
                      {formatDate(delivery.deliveredAt, timezone)}
                    </time>
                  </>
                )}
                {(delivery.state === "pending" || delivery.state === "failed") &&
                  delivery.nextAttemptAt !== null && (
                    <>
                      {delivery.state === "pending" ? "; next attempt " : " "}
                      <time dateTime={delivery.nextAttemptAt}>
                        {formatDate(delivery.nextAttemptAt, timezone)}
                      </time>
                    </>
                  )}
              </li>
            ))}
          </ul>
        </div>
      )}
      <a
        aria-label={`Open official advisory ${alert.advisoryId} for ${alert.packageName}`}
        href={alert.sourceUrl}
        rel="noopener noreferrer"
        target="_blank"
      >
        Open official advisory
      </a>
    </article>
  );
};

export const CriticalAlertCard = memo<CriticalAlertCardProps>(CriticalAlertCardComponent);
CriticalAlertCard.displayName = "CriticalAlertCard";
