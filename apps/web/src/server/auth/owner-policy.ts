export type OwnerProfile = Readonly<{
  email: string;
  emailVerified: true;
  githubUserId: number;
  image?: string;
  login: string;
  name: string;
}>;

const loginPattern = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/;

const recordValue = (value: unknown): Record<string, unknown> | null =>
  typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;

const githubID = (value: unknown): string | null => {
  const normalized = typeof value === "number" ? String(value) : value;
  if (
    typeof normalized !== "string" ||
    !/^[1-9][0-9]{0,15}$/.test(normalized) ||
    !Number.isSafeInteger(Number(normalized))
  ) {
    return null;
  }
  return normalized;
};

const profileImage = (profile: Record<string, unknown>): string | null => {
  const candidate = profile.avatar_url ?? profile.image;
  if (typeof candidate !== "string" || candidate.length === 0 || candidate.length > 2048) {
    return null;
  }
  try {
    const parsed = new URL(candidate);
    return parsed.protocol === "https:" ? parsed.toString() : null;
  } catch {
    return null;
  }
};

export const isAllowedOwnerProfile = (profile: unknown, allowedGitHubUserID: string): boolean => {
  const record = recordValue(profile);
  return record !== null && githubID(record.id) === allowedGitHubUserID;
};

type OwnerAuthenticationSource = Readonly<{
  method: string;
  oauth?:
    | Readonly<{
        profile?: Record<string, unknown> | undefined;
        providerId: string;
      }>
    | undefined;
}>;

export const isAllowedOwnerSource = (
  source: OwnerAuthenticationSource,
  allowedGitHubUserID: string,
): boolean =>
  source.method === "oauth" &&
  source.oauth?.providerId === "github" &&
  isAllowedOwnerProfile(source.oauth.profile, allowedGitHubUserID);

export const mapOwnerProfile = (
  profile: unknown,
  ownerTimezone: string,
): OwnerProfile & {
  timezone: string;
} => {
  const record = recordValue(profile);
  const id = githubID(record?.id);
  const login = record?.login;
  if (record === null || id === null || typeof login !== "string" || !loginPattern.test(login)) {
    throw new Error("GitHub returned an invalid owner profile");
  }
  const providerName = record.name;
  const name =
    typeof providerName === "string" && providerName.trim().length > 0
      ? providerName.trim().slice(0, 255)
      : login;
  const image = profileImage(record);
  return {
    email: `${id}@github.relantern.local`,
    emailVerified: true,
    githubUserId: Number(id),
    ...(image === null ? {} : { image }),
    login,
    name,
    timezone: ownerTimezone,
  };
};
