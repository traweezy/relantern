import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("the owner signs in, reaches private workspaces, and signs out", async ({ page }) => {
  const anonymousStream = await page.request.get("/api/intelligence/live");
  expect(anonymousStream.status()).toBe(401);

  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
  await page.getByRole("button", { name: "Continue with GitHub" }).click();
  await expect(page.getByText("Owner verified")).toBeVisible({ timeout: 20_000 });

  const navigation = page.getByRole("navigation", { name: "Workspace" });
  await navigation.getByRole("link", { name: /^Live\b/ }).click();
  await expect(page).toHaveURL(/\/live$/);
  await expect(page.getByRole("heading", { name: "Watch material change arrive." })).toBeVisible();
  await expect(page.locator(".live-status")).toContainText("live", { timeout: 15_000 });
  const liveAccessibility = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(liveAccessibility.violations).toEqual([]);

  for (const [label, path] of [
    ["Inbox", "/inbox"],
    ["Read Later", "/later"],
    ["Sources", "/sources"],
    ["Settings", "/settings"],
  ] as const) {
    await navigation.getByRole("link", { name: new RegExp(`^${label}\\b`) }).click();
    await expect(page).toHaveURL(new RegExp(`${path}$`));
    await expect(page.locator("main#workspace-main h1:visible").first()).toBeVisible();
  }
  await page.goto("/search");
  await expect(
    page.getByRole("heading", { name: "Find the evidence, not the noise." }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto("/settings");
  await expect(page).toHaveURL(/\/login$/);
});

test("the mobile owner can sign out without hidden account controls", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  await page.getByRole("button", { name: "Continue with GitHub" }).click();
  await expect(page.getByRole("navigation", { name: "Mobile workspace" })).toBeVisible();
  const signOut = page.locator(".mobile-account").getByRole("button", { name: "Sign out" });
  await expect(signOut).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );

  const accessibility = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(accessibility.violations).toEqual([]);

  await signOut.click();
  await expect(page).toHaveURL(/\/login$/);
});
