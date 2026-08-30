export type SnoozePreset = "tonight" | "tomorrow" | "week" | "weekend";

type ZonedParts = Readonly<{
  day: number;
  hour: number;
  minute: number;
  month: number;
  second: number;
  year: number;
}>;

const zonedParts = (instant: Date, timezone: string): ZonedParts => {
  const parts = new Intl.DateTimeFormat("en-US", {
    day: "2-digit",
    hour: "2-digit",
    hourCycle: "h23",
    minute: "2-digit",
    month: "2-digit",
    second: "2-digit",
    timeZone: timezone,
    year: "numeric",
  }).formatToParts(instant);
  const values = new Map(parts.map((part) => [part.type, part.value] as const));
  const numberPart = (name: keyof ZonedParts): number => Number(values.get(name));
  return {
    day: numberPart("day"),
    hour: numberPart("hour"),
    minute: numberPart("minute"),
    month: numberPart("month"),
    second: numberPart("second"),
    year: numberPart("year"),
  };
};

const utcShape = (parts: ZonedParts): number =>
  Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, parts.second);

const sameWallClock = (left: ZonedParts, right: ZonedParts): boolean =>
  left.year === right.year &&
  left.month === right.month &&
  left.day === right.day &&
  left.hour === right.hour &&
  left.minute === right.minute &&
  left.second === right.second;

export const zonedDateTimeToISO = (parts: ZonedParts, timezone: string): string => {
  const desiredShape = utcShape(parts);
  let candidate = desiredShape;
  for (let iteration = 0; iteration < 4; iteration += 1) {
    const correction = desiredShape - utcShape(zonedParts(new Date(candidate), timezone));
    if (correction === 0) {
      break;
    }
    candidate += correction;
  }
  if (!sameWallClock(zonedParts(new Date(candidate), timezone), parts)) {
    throw new Error("That local time does not exist in the owner timezone.");
  }
  return new Date(candidate).toISOString();
};

const withDayOffset = (current: ZonedParts, days: number, hour: number): ZonedParts => {
  const shifted = new Date(Date.UTC(current.year, current.month - 1, current.day + days));
  return {
    day: shifted.getUTCDate(),
    hour,
    minute: 0,
    month: shifted.getUTCMonth() + 1,
    second: 0,
    year: shifted.getUTCFullYear(),
  };
};

export const snoozePresetTime = (
  preset: SnoozePreset,
  timezone: string,
  now = new Date(),
): string => {
  const current = zonedParts(now, timezone);
  let target: ZonedParts;
  switch (preset) {
    case "tonight":
      target = withDayOffset(current, current.hour >= 20 ? 1 : 0, 20);
      break;
    case "tomorrow":
      target = withDayOffset(current, 1, 8);
      break;
    case "week":
      target = withDayOffset(current, 7, 8);
      break;
    case "weekend": {
      const localDate = new Date(Date.UTC(current.year, current.month - 1, current.day));
      const daysUntilSaturday = (6 - localDate.getUTCDay() + 7) % 7 || 7;
      target = withDayOffset(current, daysUntilSaturday, 9);
      break;
    }
  }
  return zonedDateTimeToISO(target, timezone);
};

export const customSnoozeTime = (value: string, timezone: string): string => {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value);
  if (match === null) {
    throw new Error("Choose a complete local date and time.");
  }
  const [, year, month, day, hour, minute] = match;
  const parts: ZonedParts = {
    day: Number(day),
    hour: Number(hour),
    minute: Number(minute),
    month: Number(month),
    second: 0,
    year: Number(year),
  };
  const normalized = new Date(utcShape(parts));
  if (
    normalized.getUTCFullYear() !== parts.year ||
    normalized.getUTCMonth() + 1 !== parts.month ||
    normalized.getUTCDate() !== parts.day ||
    normalized.getUTCHours() !== parts.hour ||
    normalized.getUTCMinutes() !== parts.minute
  ) {
    throw new Error("Choose a valid local date and time.");
  }
  return zonedDateTimeToISO(parts, timezone);
};
