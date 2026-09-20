import type { AdvisoryEcosystem, WatchedTechnology } from "@relantern/domain";
import { advisoryEcosystems } from "@relantern/domain";

const isAdvisoryEcosystem = (value: string): value is AdvisoryEcosystem =>
  (advisoryEcosystems as readonly string[]).includes(value);

export const technologiesToText = (technologies: readonly WatchedTechnology[]): string =>
  technologies
    .map(
      (technology) =>
        `${technology.technology} | ${technology.packageName} | ${technology.ecosystem ?? ""} | ${technology.currentVersion} | ${technology.versionConstraint} | ${technology.status}`,
    )
    .join("\n");

export const parseTechnologies = (value: string, existing: readonly WatchedTechnology[]) =>
  value
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line, index) => {
      const parts = line.split("|").map((part) => part.trim());
      if (parts.length !== 6) {
        throw new Error(`Current stack row ${index + 1} requires six columns.`);
      }
      const [
        technology = "",
        packageName = "",
        ecosystemText = "",
        currentVersion = "",
        versionConstraint = "",
        status = "",
      ] = parts;
      if (ecosystemText !== "" && !isAdvisoryEcosystem(ecosystemText)) {
        throw new Error(`Current stack row ${index + 1} has an unsupported advisory ecosystem.`);
      }
      const ecosystem = ecosystemText === "" ? null : ecosystemText;
      const prior = existing.find(
        (candidate) => candidate.packageName === packageName && candidate.ecosystem === ecosystem,
      );
      const preserveVerification =
        prior?.ecosystem === ecosystem &&
        prior.currentVersion === currentVersion &&
        prior.versionConstraint === versionConstraint;
      return {
        currentVersion,
        ecosystem,
        ...(prior === undefined ? {} : { id: prior.id }),
        ...(preserveVerification && prior?.lastVerifiedAt !== undefined
          ? { lastVerifiedAt: prior.lastVerifiedAt }
          : {}),
        packageName,
        source: prior?.source ?? "owner-settings",
        status,
        technology,
        versionConstraint,
      };
    });
