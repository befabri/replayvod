import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

import {
  applyDevSeed,
  CLOUD_API,
  latestMagicLinkToken,
  SEEDED_EMAIL,
} from "./_helpers";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const SERVER_DIR = path.resolve(HERE, "../../../server");
const RELAY_BASE = "ws://localhost:8788";

test.beforeAll(() => {
  applyDevSeed();
});

test("full flow: active sub → /account create token → server receives webhook through relay", async ({
  page,
}, testInfo) => {
  // 1. Subscription is already seeded by applyDevSeed (dev@local.test holds an
  //    active polar_subscription tied to POLAR_PRODUCT_MONTHLY).
  //
  // 2. Drive the magic-link login the same way a real user would.
  await page.goto("/login");
  await page.locator("#email").fill(SEEDED_EMAIL);
  const submitted = page.waitForResponse(
    (r) =>
      r.url().includes("/api/auth/sign-in/magic-link") && r.status() === 200,
  );
  await page.locator("#login-submit").click();
  await submitted;

  const verificationToken = latestMagicLinkToken(SEEDED_EMAIL);
  await page.goto(
    `${CLOUD_API}/api/auth/magic-link/verify?token=${encodeURIComponent(
      verificationToken,
    )}&callbackURL=${encodeURIComponent("http://localhost:4322/account")}`,
  );
  await expect(page.locator("#subscription-status")).toHaveText("active");

  // 3. Click Create token; the cloud worker mints a row in `relay_token`.
  const tokenIssued = page.waitForResponse(
    (r) => r.url().includes("/me/tokens") && r.status() === 200,
  );
  await page.locator("#create-token-button").click();
  await tokenIssued;

  // 4. Pick the rendered subscribe URL out of the dashboard so we exercise
  //    the same string the user copies from `/account`.
  const subscribeURL = await page
    .locator("#token-list .grid")
    .first()
    .locator("div")
    .last()
    .innerText();
  expect(subscribeURL).toMatch(/^ws:\/\/localhost:8788\/u\/[A-Za-z0-9_-]+\/subscribe$/);
  const ingestURL = subscribeURL
    .replace(/^ws:\/\//, "http://")
    .replace(/\/subscribe$/, "");
  const relayToken = subscribeURL
    .replace(/^ws:\/\/localhost:8788\/u\//, "")
    .replace(/\/subscribe$/, "");

  // 5. Run the Go integration test that boots the production relayclient
  //    agent in front of a real chi-routed webhook.Handler, fires an
  //    HMAC-signed Twitch event at the relay ingest, and asserts the
  //    EventProcessor saw the decoded notification.
  let goOutput = "";
  try {
    goOutput = execFileSync(
      "go",
      [
        "test",
        "-tags=integration",
        "-run",
        "TestRelayEndToEnd",
        "-v",
        "./internal/relayclient/...",
      ],
      {
        cwd: SERVER_DIR,
        env: {
          ...process.env,
          RELAY_TEST_TOKEN: relayToken,
          RELAY_BASE_URL: RELAY_BASE,
        },
        encoding: "utf-8",
      },
    );
  } catch (err) {
    if (err instanceof Error && "stdout" in err) {
      goOutput = String((err as { stdout?: string }).stdout ?? "");
      const stderr = String((err as { stderr?: string }).stderr ?? "");
      throw new Error(
        `Go integration test failed:\n--- stdout ---\n${goOutput}\n--- stderr ---\n${stderr}`,
      );
    }
    throw err;
  }
  expect(goOutput).toMatch(/relay end-to-end ok/);
  expect(goOutput).toMatch(/PASS: TestRelayEndToEnd/);

  // 6. Surface the Twitch console URL (ingest URL on the production relay
  //    host, since Twitch cannot reach localhost). Local ingest URL is
  //    printed alongside for reference.
  const prodIngestURL = `https://relay.replayvod.com/u/${relayToken}`;
  const summary = [
    "",
    "=== Twitch dev console — Webhook Callback URL ===",
    prodIngestURL,
    "",
    "=== Local relay ingest URL (used for this verification) ===",
    ingestURL,
    "",
    "=== server/.env values for this token ===",
    `WEBHOOK_CALLBACK_URL=${prodIngestURL}`,
    `RELAY_SUBSCRIBE_URL=wss://relay.replayvod.com/u/${relayToken}/subscribe`,
    "",
  ].join("\n");
  console.log(summary);
  await testInfo.attach("twitch-console-url.txt", {
    body: summary,
    contentType: "text/plain",
  });
});
