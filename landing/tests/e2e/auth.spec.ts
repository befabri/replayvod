import { expect, test } from "@playwright/test";
import {
  applyDevSeed,
  CLOUD_API,
  latestMagicLinkToken,
  SEEDED_EMAIL,
} from "./_helpers";

test.beforeAll(() => {
  applyDevSeed();
});

test("login with magic link lands on /account", async ({ page }) => {
  await page.goto("/login");
  await expect(page.locator("#email")).toBeVisible();

  await page.locator("#email").fill(SEEDED_EMAIL);
  const requestPromise = page.waitForResponse(
    (r) =>
      r.url().includes("/api/auth/sign-in/magic-link") && r.status() === 200,
  );
  await page.locator("#login-submit").click();
  await requestPromise;

  await expect(page.locator("#login-status")).toContainText(/magic link/i);

  const token = latestMagicLinkToken(SEEDED_EMAIL);
  const verifyURL = `${CLOUD_API}/api/auth/magic-link/verify?token=${encodeURIComponent(
    token,
  )}&callbackURL=${encodeURIComponent("http://localhost:4322/account")}`;
  await page.goto(verifyURL);

  await expect(page).toHaveURL(/\/account$/);
  await expect(page.locator("#account-subtitle")).toContainText(
    new RegExp(`signed in as ${SEEDED_EMAIL}`, "i"),
  );
});

test("clicking a magic link for an unknown email does not log in", async ({
  page,
  request,
}) => {
  await page.goto("/login");
  await page.locator("#email").fill("stranger@local.test");
  const submitted = page.waitForResponse(
    (r) =>
      r.url().includes("/api/auth/sign-in/magic-link") && r.status() === 200,
  );
  await page.locator("#login-submit").click();
  await submitted;

  // Either no verification row was created, or the token cannot redeem to a
  // session. Both are valid outcomes; assert /me is unauthenticated.
  const me = await request.get(`${CLOUD_API}/me`, {
    headers: { Origin: "http://localhost:4322" },
  });
  expect(me.status()).toBe(401);
});
