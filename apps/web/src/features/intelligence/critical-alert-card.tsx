import type { AlertDeliveryStatus, CriticalAlert } from "@relantern/domain";
import { memo } from "react";

type CriticalAlertCardProps = Readonly<{
  alert: CriticalAlert;
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
  }
};

const CriticalAlertCardComponent = ({ alert, deliveries, timezone }: CriticalAlertCardProps) => (
  <article className="critical-alert-card">
    <div className="critical-alert-card-heading">
      <span className="critical-alert-severity">Critical · {alert.ecosystem}</span>
      <time dateTime={alert.alertedAt}>{formatDate(alert.alertedAt, timezone)}</time>
    </div>
    <h3>{alert.title}</h3>
    <p>
      <strong>{alert.packageName}</strong> at {alert.currentVersion} is within the affected range{" "}
      {alert.versionRange}.
    </p>
    {alert.patchedVersion !== "" && <p>First patched version: {alert.patchedVersion}</p>}
    <p className="critical-alert-reason">{alert.reason}</p>
    {deliveries !== undefined && (
      <div className="alert-delivery-status">
        <p>Delivery</p>
        <ul>
          <li>Dashboard: Available</li>
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

export const CriticalAlertCard = memo<CriticalAlertCardProps>(CriticalAlertCardComponent);
CriticalAlertCard.displayName = "CriticalAlertCard";
