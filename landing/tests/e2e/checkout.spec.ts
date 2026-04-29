import { expect, test } from "@playwright/test";
import { applyDevSeed, CLOUD_API } from "./_helpers";

test.beforeAll(() => {
  applyDevSeed();
});

async function clickPricingCTA(
  page: import("@playwright/test").Page,
  plan: "monthly" | "yearly",
) {
  await page.goto("/#pricing");
  if (plan === "yearly") {
    await page.locator('[data-plan-tab="yearly"]').click();
  }
  await page.locator("[data-plan-cta]").click();
  await expect(page).toHaveURL(/\/checkout/);
}

test("monthly checkout shows the 14-day trial copy and redirects to Polar", async ({
  page,
  context,
}) => {
  await clickPricingCTA(page, "monthly");
  await expect(page.locator("[data-checkout-trial]")).toContainText(
    /14-day trial/i,
  );
  await expect(page.locator("[data-checkout-price]")).toContainText("$5");

  const polarURL = await new Promise<string>((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new Error("did not navigate to sandbox.polar.sh in time")),
      20_000,
    );
    context.on("request", (req) => {
      const url = req.url();
      if (url.startsWith("https://sandbox.polar.sh/")) {
        clearTimeout(timer);
        resolve(url);
      }
    });
    page.locator("#checkout-submit").click().catch(reject);
  });
  expect(polarURL).toMatch(/^https:\/\/sandbox\.polar\.sh\/checkout\//);
});

test("yearly checkout drops the trial copy and shows the yearly price", async ({
  page,
  context,
}) => {
  await clickPricingCTA(page, "yearly");
  await expect(page.locator("[data-checkout-trial]")).toContainText(/no trial/i);
  await expect(page.locator("[data-checkout-price]")).toContainText("$50");

  const polarURL = await new Promise<string>((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new Error("did not navigate to sandbox.polar.sh in time")),
      20_000,
    );
    context.on("request", (req) => {
      const url = req.url();
      if (url.startsWith("https://sandbox.polar.sh/")) {
        clearTimeout(timer);
        resolve(url);
      }
    });
    page.locator("#checkout-submit").click().catch(reject);
  });
  expect(polarURL).toMatch(/^https:\/\/sandbox\.polar\.sh\/checkout\//);
});

test("anonymous /api/auth/checkout/<plan> issues a Polar session URL", async ({
  request,
}) => {
  for (const plan of ["monthly", "yearly"] as const) {
    const res = await request.get(`${CLOUD_API}/api/auth/checkout/${plan}`, {
      maxRedirects: 0,
      headers: { Origin: "http://localhost:4322" },
    });
    expect(res.status()).toBe(303);
    expect(res.headers().location).toMatch(
      /^https:\/\/sandbox\.polar\.sh\/checkout\//,
    );
  }
});
