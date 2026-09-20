import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("the guided demo stays on its static origin and resets local triage", async ({
  page,
  baseURL,
}) => {
  const demoOrigin = new URL(baseURL ?? "").origin;
  const requests: Readonly<{ method: string; url: string }>[] = [];
  page.on("request", (request) => {
    requests.push({ method: request.method(), url: request.url() });
  });

  const response = await page.goto("/demo");
  expect(response?.status()).toBe(200);
  expect(response?.headers()["x-robots-tag"]).toContain("noindex");
  expect(response?.headers()["content-security-policy"]).not.toMatch(
    /script-src[^;]*'unsafe-inline'/,
  );

  await page.getByRole("button", { name: "Start guided demo" }).click();
  await expect(page.getByText("Guided step 1 of 4")).toBeVisible();
  await page.getByRole("button", { name: "Next step" }).click();
  await expect(page.getByText("Guided step 2 of 4")).toBeVisible();
  await page.getByRole("button", { name: "Next step" }).click();
  await expect(page.getByText("Guided step 3 of 4")).toBeVisible();

  const firstStory = page.getByRole("group", { name: "Local demo story actions" }).first();
  await firstStory.getByRole("button", { name: "Save for later" }).click();
  await expect(firstStory.getByRole("button", { name: "In Later" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await firstStory.getByRole("button", { name: "Star", exact: true }).click();
  await expect(firstStory.getByRole("button", { name: "Starred" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await page.getByRole("button", { name: "Next step" }).click();
  await expect(page.getByText("Guided step 4 of 4")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Technology radar" })).toBeVisible();
  await page.getByRole("button", { name: "Reset demo" }).click();
  await expect(
    page.getByRole("group", { name: "Local demo story actions" }).first().getByRole("button", {
      name: "Save for later",
    }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Inspect representative story" }).click();
  await expect(page).toHaveURL(/\/demo\/story\/go-toolchain-security$/);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

  for (const request of requests) {
    if (!request.url.startsWith("http")) {
      continue;
    }
    expect(new URL(request.url).origin).toBe(demoOrigin);
    expect(["GET", "HEAD"]).toContain(request.method);
    expect(new URL(request.url).pathname).not.toMatch(/^\/(api|auth)(\/|$)/);
  }
});

test("the demo excludes private routes and write methods", async ({ request }) => {
  expect((await request.get("/api/v1/stories")).status()).toBe(404);
  expect((await request.get("/login")).status()).toBe(404);
  expect((await request.post("/demo")).status()).toBe(405);
  expect((await request.head("/demo")).status()).toBe(200);
});

test("the demo is accessible at narrow width with reduced motion", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/demo");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(page.getByRole("button", { name: "Start guided demo" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );

  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(result.violations).toEqual([]);
});
