import { expect, test } from "@playwright/test";
import { mockTrpc, validSession } from "./support/trpc";

// Mocks the same-origin /trpc endpoint, so no Go backend is needed. The
// active-downloads SSE subscription is aborted by the mock (EventSource just
// retries), so the live rows never populate — but the index route still mounts
// the RunningDownloads section, which is what this exercises end to end: the
// authenticated shell + dashboard index render in a real browser.
test.describe("dashboard", () => {
	test("authenticated dashboard mounts the Running Now section", async ({
		page,
	}) => {
		await mockTrpc(page, validSession);
		await page.goto("/dashboard");
		// The profile menu confirms we reached the authenticated shell. The first
		// assertion gets a generous timeout to absorb vite dev's on-demand route
		// compile when this spec runs alone against a cold server.
		await expect(
			page.getByRole("button", { name: "Open user menu" }),
		).toBeVisible({ timeout: 30_000 });
		// The heading lives outside the loading/empty/error branch, so it renders
		// regardless of the (aborted) subscription state.
		await expect(
			page.getByRole("heading", { name: "Running now" }),
		).toBeVisible();
	});
});
