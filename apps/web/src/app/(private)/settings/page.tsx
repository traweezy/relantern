import type { Metadata } from "next";
import { SettingsWorkspace } from "@/features/control-plane/settings-workspace";
import { requireOwnerSession } from "@/server/auth/session";
import { getSettings } from "@/server/controlplane/client";

export const metadata: Metadata = { title: "Settings | Relantern" };

const SettingsPage = async () => {
  const owner = await requireOwnerSession();
  const settings = await getSettings(owner.userID).catch(() => null);
  return (
    <>
      <header className="intelligence-header control-plane-header">
        <div>
          <p className="eyebrow">Settings · Owner authority</p>
          <h1>Shape the signal and its timing.</h1>
          <p>
            Tune interests, current stack, quiet hours, cost and retention bounds, and durable
            local-time schedules from one audited workspace.
          </p>
        </div>
        {settings !== null && (
          <div className="control-plane-stamp">
            <span>Owner timezone</span>
            <strong>{settings.owner.timezone}</strong>
          </div>
        )}
      </header>
      {settings === null ? (
        <div className="reading-empty-state">
          <p className="eyebrow">Control plane degraded</p>
          <h2>Owner settings are temporarily unavailable.</h2>
          <p>The page failed closed rather than presenting stale mutation controls.</p>
        </div>
      ) : (
        <SettingsWorkspace initialSettings={settings} />
      )}
    </>
  );
};

export default SettingsPage;
