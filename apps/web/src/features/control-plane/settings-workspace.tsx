"use client";

import type {
  ScheduleActionResult,
  ScheduleDefinition,
  SchedulePreview,
  SettingsSnapshot,
  WatchedTechnology,
} from "@relantern/domain";
import { useForm } from "@tanstack/react-form";
import type { FormEvent } from "react";
import { memo, useCallback, useMemo, useState } from "react";
import { scheduleCommandSchema, settingsCommandSchema } from "@/features/control-plane/commands";
import {
  parseScheduleActionResult,
  parseScheduleDefinition,
  parseSchedulePreview,
  parseSettingsSnapshot,
} from "@/features/control-plane/contract";

type SettingsWorkspaceProps = Readonly<{
  initialSettings: SettingsSnapshot;
}>;

const requestJSON = async <T,>(
  path: string,
  body: unknown | undefined,
  method: "GET" | "POST" | "PUT",
  parse: (value: unknown) => T,
): Promise<T> => {
  const response = await fetch(path, {
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    headers: {
      accept: "application/json",
      ...(body === undefined ? {} : { "content-type": "application/json" }),
    },
    method,
    signal: AbortSignal.timeout(15_000),
  });
  const decoded: unknown = await response.json();
  if (!response.ok) {
    const detail =
      typeof decoded === "object" && decoded !== null && "detail" in decoded
        ? String(decoded.detail)
        : "The settings request failed.";
    throw new Error(detail);
  }
  return parse(decoded);
};

const joinValues = (values: readonly string[]): string => values.join(", ");

const splitValues = (value: string): string[] =>
  value
    .split(",")
    .map((entry) => entry.trim())
    .filter((entry, index, entries) => entry !== "" && entries.indexOf(entry) === index);

const topicsToText = (settings: SettingsSnapshot): string =>
  settings.profile.topics
    .map(
      (topic) =>
        `${topic.topicId} | ${topic.priority} | ${topic.weight} | ${joinValues(topic.keywords)} | ${joinValues(topic.exclusions)}`,
    )
    .join("\n");

const technologiesToText = (settings: SettingsSnapshot): string =>
  settings.technologies
    .map(
      (technology) =>
        `${technology.technology} | ${technology.packageName} | ${technology.currentVersion} | ${technology.versionConstraint} | ${technology.status}`,
    )
    .join("\n");

const parseTopics = (value: string) =>
  value
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [topicId = "", priority = "", weight = "", keywords = "", exclusions = ""] = line
        .split("|")
        .map((part) => part.trim());
      return {
        exclusions: splitValues(exclusions),
        keywords: splitValues(keywords),
        priority: Number(priority),
        topicId,
        weight: Number(weight),
      };
    });

const parseTechnologies = (value: string, existing: readonly WatchedTechnology[]) =>
  value
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [
        technology = "",
        packageName = "",
        currentVersion = "",
        versionConstraint = "",
        status = "",
      ] = line.split("|").map((part) => part.trim());
      const prior = existing.find((candidate) => candidate.packageName === packageName);
      return {
        currentVersion,
        ...(prior === undefined ? {} : { id: prior.id }),
        ...(prior?.lastVerifiedAt === undefined ? {} : { lastVerifiedAt: prior.lastVerifiedAt }),
        packageName,
        source: prior?.source ?? "owner-settings",
        status,
        technology,
        versionConstraint,
      };
    });

type OwnerSettingsFormProps = Readonly<{
  onChange: (settings: SettingsSnapshot) => void;
  settings: SettingsSnapshot;
}>;

const OwnerSettingsFormComponent = ({ onChange, settings }: OwnerSettingsFormProps) => {
  const [notice, setNotice] = useState(
    "Settings updates are versioned, audited, and applied only to owner-scoped records.",
  );
  const settingsForm = useForm({
    defaultValues: {
      auditRetentionDays: String(settings.owner.auditRetentionDays),
      criticalAlertsBypass: settings.owner.criticalAlertsBypass,
      monthlyHardBudgetUsd: settings.owner.monthlyHardBudgetUsd,
      monthlySoftBudgetUsd: settings.owner.monthlySoftBudgetUsd,
      profileName: settings.profile.name,
      profileSummary: settings.profile.profileSummary,
      quietHoursEnd: settings.owner.quietHoursEnd,
      quietHoursStart: settings.owner.quietHoursStart,
      rawRetentionDays: String(settings.owner.rawRetentionDays),
      technologiesText: technologiesToText(settings),
      timezone: settings.owner.timezone,
      topicsText: topicsToText(settings),
    },
    onSubmit: async ({ value }) => {
      const command = settingsCommandSchema.parse({
        auditRetentionDays: Number(value.auditRetentionDays),
        criticalAlertsBypass: value.criticalAlertsBypass,
        expectedVersion: settings.owner.version,
        monthlyHardBudgetUsd: value.monthlyHardBudgetUsd,
        monthlySoftBudgetUsd: value.monthlySoftBudgetUsd,
        profileName: value.profileName,
        profileSummary: value.profileSummary,
        quietHoursEnd: value.quietHoursEnd,
        quietHoursStart: value.quietHoursStart,
        rawRetentionDays: Number(value.rawRetentionDays),
        technologies: parseTechnologies(value.technologiesText, settings.technologies),
        timezone: value.timezone,
        topics: parseTopics(value.topicsText),
      });
      const updated = await requestJSON(
        "/api/control-plane/settings",
        command,
        "PUT",
        parseSettingsSnapshot,
      );
      onChange(updated);
      setNotice(`Settings version ${updated.owner.version} saved.`);
    },
  });
  const handleSubmit = useCallback(
    (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void settingsForm.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The owner settings were not saved.");
      });
    },
    [settingsForm],
  );

  return (
    <form className="owner-settings-form" onSubmit={handleSubmit}>
      <section
        aria-labelledby="interest-settings-title"
        className="settings-card settings-card-wide"
      >
        <div className="settings-card-heading">
          <div>
            <p className="eyebrow">Personalization boundary</p>
            <h2 id="interest-settings-title">Interests and current stack</h2>
          </div>
          <span>Profile v{settings.profile.version}</span>
        </div>
        <div className="settings-field-grid">
          <settingsForm.Field name="profileName">
            {(field) => (
              <label>
                Profile name
                <input
                  maxLength={120}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="text"
                  value={field.state.value}
                />
              </label>
            )}
          </settingsForm.Field>
          <settingsForm.Field name="timezone">
            {(field) => (
              <label>
                Owner timezone
                <input
                  maxLength={255}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="text"
                  value={field.state.value}
                />
              </label>
            )}
          </settingsForm.Field>
        </div>
        <settingsForm.Field name="profileSummary">
          {(field) => (
            <label>
              Profile summary
              <textarea
                maxLength={2000}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
                rows={3}
                value={field.state.value}
              />
            </label>
          )}
        </settingsForm.Field>
        <settingsForm.Field name="topicsText">
          {(field) => (
            <label>
              Interest topics
              <textarea
                aria-describedby="topic-format-help"
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
                rows={7}
                value={field.state.value}
              />
              <small id="topic-format-help">
                One per line: topic-id | priority 1–5 | weight 0–1 | comma keywords | comma
                exclusions
              </small>
            </label>
          )}
        </settingsForm.Field>
        <settingsForm.Field name="technologiesText">
          {(field) => (
            <label>
              Current stack
              <textarea
                aria-describedby="technology-format-help"
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
                rows={8}
                value={field.state.value}
              />
              <small id="technology-format-help">
                One per line: technology | package | current version | version constraint |
                active/evaluating/legacy/planned
              </small>
            </label>
          )}
        </settingsForm.Field>
      </section>

      <section aria-labelledby="policy-settings-title" className="settings-card">
        <div className="settings-card-heading">
          <div>
            <p className="eyebrow">Notification policy</p>
            <h2 id="policy-settings-title">Quiet hours and budgets</h2>
          </div>
          <span>Owner v{settings.owner.version}</span>
        </div>
        <div className="settings-field-grid">
          <settingsForm.Field name="quietHoursStart">
            {(field) => (
              <label>
                Quiet hours start
                <input
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="time"
                  value={field.state.value}
                />
              </label>
            )}
          </settingsForm.Field>
          <settingsForm.Field name="quietHoursEnd">
            {(field) => (
              <label>
                Quiet hours end
                <input
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="time"
                  value={field.state.value}
                />
              </label>
            )}
          </settingsForm.Field>
          <settingsForm.Field name="monthlySoftBudgetUsd">
            {(field) => (
              <label>
                Monthly soft budget (USD)
                <input
                  inputMode="decimal"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="text"
                  value={field.state.value}
                />
              </label>
            )}
          </settingsForm.Field>
          <settingsForm.Field name="monthlyHardBudgetUsd">
            {(field) => (
              <label>
                Monthly hard cap (USD)
                <input
                  inputMode="decimal"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="text"
                  value={field.state.value}
                />
              </label>
            )}
          </settingsForm.Field>
          <settingsForm.Field name="rawRetentionDays">
            {(field) => (
              <label>
                Raw retention days
                <input
                  max="3650"
                  min="7"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="number"
                  value={field.state.value}
                />
              </label>
            )}
          </settingsForm.Field>
          <settingsForm.Field name="auditRetentionDays">
            {(field) => (
              <label>
                Audit retention days
                <input
                  max="3650"
                  min="30"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="number"
                  value={field.state.value}
                />
              </label>
            )}
          </settingsForm.Field>
        </div>
        <settingsForm.Field name="criticalAlertsBypass">
          {(field) => (
            <label className="control-check">
              <input
                checked={field.state.value}
                onChange={(event) => field.handleChange(event.target.checked)}
                type="checkbox"
              />
              Confirmed critical watched-dependency alerts may bypass quiet hours
            </label>
          )}
        </settingsForm.Field>
      </section>

      <footer className="settings-save-bar">
        <p aria-live="polite">{notice}</p>
        <settingsForm.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
          {([canSubmit, isSubmitting]) => (
            <button disabled={!canSubmit || isSubmitting} type="submit">
              {isSubmitting ? "Saving settings…" : "Save owner settings"}
            </button>
          )}
        </settingsForm.Subscribe>
      </footer>
    </form>
  );
};

const OwnerSettingsForm = memo<OwnerSettingsFormProps>(OwnerSettingsFormComponent);
OwnerSettingsForm.displayName = "OwnerSettingsForm";

type ScheduleCardProps = Readonly<{
  onChange: (schedule: ScheduleDefinition) => void;
  schedule: ScheduleDefinition;
}>;

const scheduleLabel = (scheduleType: ScheduleDefinition["scheduleType"]): string =>
  scheduleType
    .split("_")
    .map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`)
    .join(" ");

const ScheduleCardComponent = ({ onChange, schedule }: ScheduleCardProps) => {
  const [notice, setNotice] = useState("No provider is contacted by Preview or Run now in PR15.");
  const [preview, setPreview] = useState<SchedulePreview | null>(null);
  const [actionPending, setActionPending] = useState(false);
  const [pausedUntil, setPausedUntil] = useState("");
  const [runKey, setRunKey] = useState(() => crypto.randomUUID());
  const scheduleForm = useForm({
    defaultValues: {
      catchupGraceMinutes: String(schedule.catchupGraceMinutes),
      catchupPolicy: schedule.catchupPolicy,
      channels: schedule.channels.join(","),
      daysOfWeek: schedule.daysOfWeek.join(","),
      emptyBehavior: schedule.emptyBehavior,
      enabled: schedule.enabled,
      includeComingSoon: schedule.includeComingSoon,
      includeLaterReminders: schedule.includeLaterReminders,
      includeRadarCandidates: schedule.includeRadarCandidates,
      localTime: schedule.localTime,
      maximumItems: String(schedule.maximumItems),
      minimumScore: String(schedule.minimumScore),
      timezone: schedule.timezone,
      weekendMode: schedule.weekendMode,
    },
    onSubmit: async ({ value }) => {
      const command = scheduleCommandSchema.parse({
        catchupGraceMinutes: Number(value.catchupGraceMinutes),
        catchupPolicy: value.catchupPolicy,
        channels: splitValues(value.channels),
        daysOfWeek: splitValues(value.daysOfWeek).map(Number),
        emptyBehavior: value.emptyBehavior,
        enabled: value.enabled,
        expectedVersion: schedule.version,
        includeComingSoon: value.includeComingSoon,
        includeLaterReminders: value.includeLaterReminders,
        includeRadarCandidates: value.includeRadarCandidates,
        localTime: value.localTime,
        maximumItems: Number(value.maximumItems),
        minimumScore: Number(value.minimumScore),
        timezone: value.timezone,
        weekendMode: value.weekendMode,
      });
      const updated = await requestJSON(
        `/api/control-plane/settings/schedules/${schedule.id}`,
        command,
        "PUT",
        parseScheduleDefinition,
      );
      onChange(updated);
      setNotice(`Schedule version ${updated.version} saved with a recalculated next occurrence.`);
    },
  });
  const handleSubmit = useCallback(
    (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      event.stopPropagation();
      void scheduleForm.handleSubmit().catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The schedule was not saved.");
      });
    },
    [scheduleForm],
  );
  const loadPreview = useCallback(() => {
    if (actionPending) return;
    setActionPending(true);
    void requestJSON(
      `/api/control-plane/settings/schedules/${schedule.id}/preview`,
      undefined,
      "GET",
      parseSchedulePreview,
    )
      .then((result) => {
        setPreview(result);
        setNotice("Preview loaded without creating an occurrence or contacting a provider.");
      })
      .catch((error: unknown) => {
        setNotice(error instanceof Error ? error.message : "The schedule preview failed.");
      })
      .finally(() => setActionPending(false));
  }, [actionPending, schedule.id]);
  const runAction = useCallback(
    (action: "pause" | "resume" | "run_now" | "skip_next") => {
      if (actionPending) return;
      let pausedUntilValue: string | undefined;
      if (action === "pause") {
        const parsed = new Date(pausedUntil);
        if (pausedUntil === "" || Number.isNaN(parsed.valueOf())) {
          setNotice("Choose a valid future pause end before pausing.");
          return;
        }
        pausedUntilValue = parsed.toISOString();
      }
      setActionPending(true);
      const command = {
        action,
        ...(action === "run_now" ? { idempotencyKey: runKey } : {}),
        ...(pausedUntilValue === undefined ? {} : { pausedUntil: pausedUntilValue }),
        reason: `Owner requested ${action.replace("_", " ")} from Settings`,
      };
      void requestJSON<ScheduleActionResult>(
        `/api/control-plane/settings/schedules/${schedule.id}/actions`,
        command,
        "POST",
        parseScheduleActionResult,
      )
        .then((result) => {
          onChange(result.schedule);
          setNotice(result.message);
          if (action === "run_now") setRunKey(crypto.randomUUID());
        })
        .catch((error: unknown) => {
          setNotice(error instanceof Error ? error.message : "The schedule action failed.");
        })
        .finally(() => setActionPending(false));
    },
    [actionPending, onChange, pausedUntil, runKey, schedule.id],
  );
  const handleRunNow = useCallback(() => runAction("run_now"), [runAction]);
  const handleSkip = useCallback(() => runAction("skip_next"), [runAction]);
  const handlePause = useCallback(() => runAction("pause"), [runAction]);
  const handleResume = useCallback(() => runAction("resume"), [runAction]);
  const handlePausedUntil = useCallback((event: React.ChangeEvent<HTMLInputElement>) => {
    setPausedUntil(event.target.value);
  }, []);
  const clearPausedUntil = useCallback(() => setPausedUntil(""), []);

  return (
    <article className="schedule-card">
      <header>
        <div>
          <p className="eyebrow">Durable database schedule</p>
          <h3>{scheduleLabel(schedule.scheduleType)}</h3>
        </div>
        <span
          className={`schedule-state ${schedule.pausedAt === undefined ? "is-active" : "is-paused"}`}
        >
          {schedule.pausedAt === undefined ? (schedule.enabled ? "Enabled" : "Disabled") : "Paused"}
        </span>
      </header>
      <p className="schedule-next">
        Next due{" "}
        {new Intl.DateTimeFormat("en-US", {
          dateStyle: "medium",
          timeStyle: "short",
          timeZone: schedule.timezone,
        }).format(new Date(schedule.nextDueAt))}
      </p>
      <form className="schedule-form" onSubmit={handleSubmit}>
        <div className="settings-field-grid">
          <scheduleForm.Field name="timezone">
            {(field) => (
              <label>
                Timezone
                <input
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="text"
                  value={field.state.value}
                />
              </label>
            )}
          </scheduleForm.Field>
          <scheduleForm.Field name="localTime">
            {(field) => (
              <label>
                Local time
                <input
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="time"
                  value={field.state.value}
                />
              </label>
            )}
          </scheduleForm.Field>
          <scheduleForm.Field name="daysOfWeek">
            {(field) => (
              <label>
                ISO weekdays
                <input
                  aria-describedby={`${schedule.id}-weekday-help`}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="text"
                  value={field.state.value}
                />
                <small id={`${schedule.id}-weekday-help`}>
                  1 Monday through 7 Sunday, comma-separated.
                </small>
              </label>
            )}
          </scheduleForm.Field>
          <scheduleForm.Field name="weekendMode">
            {(field) => (
              <label>
                Weekend mode
                <select
                  onBlur={field.handleBlur}
                  onChange={(event) =>
                    field.handleChange(event.target.value as typeof field.state.value)
                  }
                  value={field.state.value}
                >
                  <option value="normal">Normal</option>
                  <option value="weekly_only">Weekly only</option>
                  <option value="off">Off</option>
                </select>
              </label>
            )}
          </scheduleForm.Field>
          <scheduleForm.Field name="maximumItems">
            {(field) => (
              <label>
                Maximum items
                <select
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  value={field.state.value}
                >
                  <option value="5">5</option>
                  <option value="10">10</option>
                  <option value="15">15</option>
                  <option value="20">20</option>
                </select>
              </label>
            )}
          </scheduleForm.Field>
          <scheduleForm.Field name="minimumScore">
            {(field) => (
              <label>
                Minimum score
                <input
                  max="1"
                  min="0"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  step="0.05"
                  type="number"
                  value={field.state.value}
                />
              </label>
            )}
          </scheduleForm.Field>
          <scheduleForm.Field name="catchupPolicy">
            {(field) => (
              <label>
                Catch-up policy
                <select
                  onBlur={field.handleBlur}
                  onChange={(event) =>
                    field.handleChange(event.target.value as typeof field.state.value)
                  }
                  value={field.state.value}
                >
                  <option value="catch_up">Catch up</option>
                  <option value="skip">Skip</option>
                </select>
              </label>
            )}
          </scheduleForm.Field>
          <scheduleForm.Field name="catchupGraceMinutes">
            {(field) => (
              <label>
                Catch-up grace (minutes)
                <input
                  max="10080"
                  min="0"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="number"
                  value={field.state.value}
                />
              </label>
            )}
          </scheduleForm.Field>
          <scheduleForm.Field name="emptyBehavior">
            {(field) => (
              <label>
                Empty digest
                <select
                  onBlur={field.handleBlur}
                  onChange={(event) =>
                    field.handleChange(event.target.value as typeof field.state.value)
                  }
                  value={field.state.value}
                >
                  <option value="dashboard_only">Dashboard only</option>
                  <option value="all_clear">Send all-clear</option>
                  <option value="send_nothing">Send nothing</option>
                </select>
              </label>
            )}
          </scheduleForm.Field>
          <scheduleForm.Field name="channels">
            {(field) => (
              <label>
                Channels
                <input
                  aria-describedby={`${schedule.id}-channel-help`}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="text"
                  value={field.state.value}
                />
                <small id={`${schedule.id}-channel-help`}>
                  dashboard, discord, email. External delivery starts in PR17.
                </small>
              </label>
            )}
          </scheduleForm.Field>
        </div>
        <div className="schedule-check-grid">
          {(
            [
              ["enabled", "Enabled"],
              ["includeComingSoon", "Coming Soon"],
              ["includeRadarCandidates", "Radar candidates"],
              ["includeLaterReminders", "Later reminders"],
            ] as const
          ).map(([name, label]) => (
            <scheduleForm.Field key={name} name={name}>
              {(field) => (
                <label className="control-check">
                  <input
                    checked={field.state.value}
                    onChange={(event) => field.handleChange(event.target.checked)}
                    type="checkbox"
                  />
                  {label}
                </label>
              )}
            </scheduleForm.Field>
          ))}
        </div>
        <scheduleForm.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
          {([canSubmit, isSubmitting]) => (
            <button disabled={!canSubmit || isSubmitting} type="submit">
              {isSubmitting ? "Saving…" : "Save schedule"}
            </button>
          )}
        </scheduleForm.Subscribe>
      </form>
      <section
        aria-label={`${scheduleLabel(schedule.scheduleType)} controls`}
        className="schedule-actions"
      >
        <div className="schedule-action-row">
          <button disabled={actionPending} onClick={loadPreview} type="button">
            Preview next
          </button>
          <button disabled={actionPending} onClick={handleRunNow} type="button">
            Run now preview
          </button>
          <button
            disabled={actionPending || schedule.skipNextAt !== undefined}
            onClick={handleSkip}
            type="button"
          >
            Skip next
          </button>
          {schedule.pausedAt === undefined ? (
            <button
              disabled={actionPending || pausedUntil === ""}
              onClick={handlePause}
              type="button"
            >
              Pause until
            </button>
          ) : (
            <button disabled={actionPending} onClick={handleResume} type="button">
              Resume
            </button>
          )}
        </div>
        <label>
          Pause end
          <span className="input-with-clear">
            <input onChange={handlePausedUntil} type="datetime-local" value={pausedUntil} />
            <button onClick={clearPausedUntil} type="button">
              Clear
            </button>
          </span>
        </label>
        {preview !== null && (
          <div className="schedule-preview">
            <strong>
              {preview.candidateCount} eligible of {preview.maximumItems} maximum
            </strong>
            <span>
              {preview.localDate} · delivery {preview.externalDelivery ? "enabled" : "off"}
            </span>
            <p>{preview.explanation}</p>
          </div>
        )}
        <p aria-live="polite" className="control-notice">
          {notice}
        </p>
      </section>
    </article>
  );
};

const ScheduleCard = memo<ScheduleCardProps>(ScheduleCardComponent);
ScheduleCard.displayName = "ScheduleCard";

const SettingsWorkspaceComponent = ({ initialSettings }: SettingsWorkspaceProps) => {
  const [settings, setSettings] = useState(initialSettings);
  const handleSettingsChange = useCallback((updated: SettingsSnapshot) => setSettings(updated), []);
  const handleScheduleChange = useCallback((updated: ScheduleDefinition) => {
    setSettings((current) => ({
      ...current,
      schedules: current.schedules.map((schedule) =>
        schedule.id === updated.id ? updated : schedule,
      ),
    }));
  }, []);
  const sortedSchedules = useMemo(
    () =>
      [...settings.schedules].sort((left, right) =>
        left.scheduleType.localeCompare(right.scheduleType),
      ),
    [settings.schedules],
  );

  return (
    <div className="settings-workspace">
      <OwnerSettingsForm onChange={handleSettingsChange} settings={settings} />
      <section aria-labelledby="schedule-settings-title" className="control-plane-section">
        <div className="collection-toolbar">
          <div>
            <p className="eyebrow">Occurrence ledger authority</p>
            <h2 id="schedule-settings-title">Digest schedules</h2>
          </div>
          <p>Local-time rules become immutable UTC occurrences only when due.</p>
        </div>
        <div className="schedule-grid">
          {sortedSchedules.map((schedule) => (
            <ScheduleCard key={schedule.id} onChange={handleScheduleChange} schedule={schedule} />
          ))}
        </div>
      </section>
    </div>
  );
};

export const SettingsWorkspace = memo<SettingsWorkspaceProps>(SettingsWorkspaceComponent);
SettingsWorkspace.displayName = "SettingsWorkspace";
