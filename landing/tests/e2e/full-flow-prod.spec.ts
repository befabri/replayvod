import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

import { latestMagicLinkToken } from "./_helpers";

// Drives the full real-prod flow: replayvod.com → api.replayvod.com →
// relay.replayvod.com. Login is gated to bfabri@pm.me by ALLOWED_EMAILS on
// the deployed cloud worker, and the deployed cloud has DEV_MODE=1 so
// magic-link emails are skipped — the verification row is read directly
// from prod D1 via `wrangler d1 execute --remote`.
//
// Pre-requisite seeded by hand earlier (one-shot setup):
//   id='prod-bfabri', email='bfabri@pm.me' in `user`
//   active polar_subscription tied to POLAR_PRODUCT_MONTHLY
// Both already inserted via wrangler against the deployed D1.

const HERE = path.dirname(fileURLToPath(import.meta.url));
const RELAY_DIR = path.resolve(HERE, "../../../relay");

const PROD_LANDING = "https://replayvod.com";
const PROD_API = "https://api.replayvod.com";
const PROD_RELAY = "https://relay.replayvod.com";
const PROD_RELAY_WS = "wss://relay.replayvod.com";
const PROD_EMAIL = "bfabri@pm.me";

test.use({ baseURL: PROD_LANDING });

test("full flow on prod: real login → /account token → prod relay round-trip", async ({
  page,
  request,
}, testInfo) => {
  // 1. Trigger a magic-link email request through real prod cloud. With
  //    DEV_MODE=1 on the deployed worker this no-ops the EMAIL binding and
  //    only inserts the verification row in D1 — perfect for automation.
  await page.goto(`${PROD_LANDING}/login/`);
  await page.locator("#email").fill(PROD_EMAIL);
  const submitted = page.waitForResponse(
    (r) =>
      r.url().includes("/api/auth/sign-in/magic-link") && r.status() === 200,
  );
  await page.locator("#login-submit").click();
  await submitted;

  // 2. Pull the verification token straight from the deployed D1.
  const verificationToken = latestMagicLinkToken(PROD_EMAIL, { remote: true });
  await page.goto(
    `${PROD_API}/api/auth/magic-link/verify?token=${encodeURIComponent(
      verificationToken,
    )}&callbackURL=${encodeURIComponent(`${PROD_LANDING}/account`)}`,
  );
  await expect(page.locator("#subscription-status")).toHaveText("active");

  // 3. Click Create token. Cloud will mint a fresh row in `relay_token`.
  const tokenIssued = page.waitForResponse(
    (r) => r.url().includes("/me/tokens") && r.status() === 200,
  );
  await page.locator("#create-token-button").click();
  await tokenIssued;

  // 4. Pick the rendered subscribe URL out of the dashboard.
  const subscribeURL = await page
    .locator("#token-list .grid")
    .first()
    .locator("div")
    .last()
    .innerText();
  expect(subscribeURL).toMatch(
    /^wss:\/\/relay\.replayvod\.com\/u\/[A-Za-z0-9_-]+\/subscribe$/,
  );
  const relayToken = subscribeURL
    .replace(/^wss:\/\/relay\.replayvod\.com\/u\//, "")
    .replace(/\/subscribe$/, "");
  const ingestURL = `${PROD_RELAY}/u/${relayToken}`;

  // 5. Cross-check the prod relay actually validates this token against the
  //    deployed cloud (TOKEN_VALIDATE_URL is set on relay).
  const bogus = await request.post(`${PROD_RELAY}/u/0000000000000000bogus`, {
    headers: { "Content-Type": "application/json" },
    data: {},
    maxRedirects: 0,
  });
  expect(bogus.status()).toBe(403);

  // 6. Run the synthetic Twitch round-trip against prod relay using the
  //    real cloud-issued token. Async forward + verification challenge both
  //    have to pass.
  let scriptOut = "";
  try {
    scriptOut = execFileSync("npm", ["run", "verify:prod", "--silent"], {
      cwd: RELAY_DIR,
      env: {
        ...process.env,
        RELAY_BASE_URL: PROD_RELAY,
        RELAY_TOKEN: relayToken,
      },
      encoding: "utf-8",
    });
  } catch (err) {
    if (err instanceof Error && "stdout" in err) {
      scriptOut = String((err as { stdout?: string }).stdout ?? "");
      const stderr = String((err as { stderr?: string }).stderr ?? "");
      throw new Error(
        `prod relay verify failed:\n--- stdout ---\n${scriptOut}\n--- stderr ---\n${stderr}`,
      );
    }
    throw err;
  }
  expect(scriptOut).toMatch(/PASS\s+async forward/);
  expect(scriptOut).toMatch(/PASS\s+verification challenge/);
  expect(scriptOut).toMatch(/2 passed, 0 failed/);

  // 7. Print the values to paste into Twitch + server/.env.
  const summary = [
    "",
    "=== Twitch dev console — Webhook Callback URL ===",
    ingestURL,
    "",
    "=== server/.env ===",
    `WEBHOOK_CALLBACK_URL=${ingestURL}`,
    `RELAY_SUBSCRIBE_URL=${PROD_RELAY_WS}/u/${relayToken}/subscribe`,
    "# RELAY_LOCAL_CALLBACK_URL=  (defaults to http://127.0.0.1:8080/api/v1/webhook/callback)",
    "HMAC_SECRET=<random 32+ byte hex; same value you give Twitch when creating the EventSub subscription>",
    "",
    "Register the EventSub subscription against the URL above. The prod relay",
    "now validates tokens against api.replayvod.com so this token is the only",
    "way to ingest into your durable object.",
    "",
  ].join("\n");
  // eslint-disable-next-line no-console
  console.log(summary);
  await testInfo.attach("twitch-console-url-prod.txt", {
    body: summary,
    contentType: "text/plain",
  });
});
