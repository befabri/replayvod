import { expect, test } from "@playwright/test";
import {
  applyDevSeed,
  CLOUD_API,
  latestMagicLinkToken,
  RELAY,
  SEEDED_EMAIL,
} from "./_helpers";

test.beforeAll(() => {
  applyDevSeed();
});

test("seeded subscriber can issue and revoke a token from /account", async ({
  page,
  request,
}) => {
  // Drive the magic-link flow up to /account, just like a real user.
  await page.goto("/login");
  await page.locator("#email").fill(SEEDED_EMAIL);
  const submitted = page.waitForResponse(
    (r) =>
      r.url().includes("/api/auth/sign-in/magic-link") && r.status() === 200,
  );
  await page.locator("#login-submit").click();
  await submitted;

  const token = latestMagicLinkToken(SEEDED_EMAIL);
  await page.goto(
    `${CLOUD_API}/api/auth/magic-link/verify?token=${encodeURIComponent(
      token,
    )}&callbackURL=${encodeURIComponent("http://localhost:4322/account")}`,
  );

  await expect(page.locator("#subscription-status")).toHaveText("active");

  await page.locator("#create-token-button").click();
  await expect(page.locator("#token-list")).toContainText(/created/i);

  // The relay subscribe URL rendered for the new token must be reachable
  // and validated through cloud (relay → cloud /relay/tokens/validate).
  const renderedSubscribeURL = await page
    .locator("#token-list .grid")
    .first()
    .locator("div")
    .last()
    .innerText();
  expect(renderedSubscribeURL).toMatch(/^ws:\/\/localhost:8788\/u\/[A-Za-z0-9_-]+\/subscribe$/);

  const ingestURL = renderedSubscribeURL
    .replace(/^ws:\/\//, "http://")
    .replace(/\/subscribe$/, "");
  const ingest = await request.post(ingestURL, {
    headers: { "Content-Type": "application/json" },
    data: { hello: "world" },
  });
  expect(ingest.status()).toBe(202);

  // Revoke and assert the relay refuses the token afterwards.
  await page.locator(".revoke-token").first().click();
  await expect(page.locator("#token-list")).toContainText(
    /no active relay tokens/i,
    { timeout: 5_000 },
  );
  const ingestAfter = await request.post(ingestURL, {
    headers: { "Content-Type": "application/json" },
    data: {},
  });
  expect(ingestAfter.status()).toBe(403);
});

test("sign-out clears the session", async ({ page, request }) => {
  await page.goto("/login");
  await page.locator("#email").fill(SEEDED_EMAIL);
  await page.locator("#login-submit").click();
  await page.waitForResponse(
    (r) =>
      r.url().includes("/api/auth/sign-in/magic-link") && r.status() === 200,
  );

  const token = latestMagicLinkToken(SEEDED_EMAIL);
  await page.goto(
    `${CLOUD_API}/api/auth/magic-link/verify?token=${encodeURIComponent(
      token,
    )}&callbackURL=${encodeURIComponent("http://localhost:4322/account")}`,
  );

  const cookies = await page.context().cookies();
  const sessionCookie = cookies.find((c) =>
    c.name.startsWith("better-auth.session_token"),
  );
  expect(sessionCookie).toBeTruthy();

  const signedOut = page.waitForResponse(
    (r) =>
      r.url().includes("/api/auth/sign-out") && r.status() === 200,
  );
  await page.locator("#logout-button").click();
  await signedOut;

  // /me with the same cookies should now be 401.
  const me = await request.get(`${CLOUD_API}/me`, {
    headers: {
      Origin: "http://localhost:4322",
      Cookie: `${sessionCookie!.name}=${sessionCookie!.value}`,
    },
  });
  expect(me.status()).toBe(401);
});

// Quick anchor check, separate from the full UI flow above.
test("relay rejects a bogus token", async ({ request }) => {
  const res = await request.post(`${RELAY}/u/0000000000000000bogus`, {
    headers: { "Content-Type": "application/json" },
    data: {},
  });
  expect(res.status()).toBe(403);
});
