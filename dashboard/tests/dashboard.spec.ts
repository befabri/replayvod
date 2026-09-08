import { expect, test } from "@playwright/test";
import { mockTrpc, trpcOk, validSession } from "./support/trpc";
import { userState, videoRecording } from "./support/watch";

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
		// The profile menu confirms we reached the authenticated shell.
		await expect(
			page.getByRole("button", { name: "Open user menu" }),
		).toBeVisible({ timeout: 30_000 });
		// The heading lives outside the loading/empty/error branch, so it renders
		// regardless of the (aborted) subscription state.
		await expect(
			page.getByRole("heading", { name: "Running now" }),
		).toBeVisible();
		// Nothing started: no continue-watching strip.
		await expect(page.getByTestId("continue-watching")).toHaveCount(0);
	});

	test("lists the recordings the viewer is partway through", async ({
		page,
	}) => {
		await mockTrpc(page, (procs, url) => {
			const session = validSession(procs, url);
			if (session) return session;
			if (!procs.includes("video.continueWatching")) return null;
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.continueWatching"
							? [
									videoRecording(41, 3600, { user_state: userState(900) }),
									videoRecording(42, 3600, { user_state: userState(3590) }),
								]
							: null,
					),
				),
			};
		});
		await page.goto("/dashboard");
		const strip = page.getByTestId("continue-watching");
		await expect(strip).toBeVisible({ timeout: 30_000 });
		await expect(
			strip.getByRole("heading", { name: "Continue watching" }),
		).toBeVisible();
		// The one played to the end opens at the start, so it is not offered.
		await expect(strip.getByTestId("video-card-progress")).toHaveCount(1);
		await expect(strip.getByTestId("video-card-progress")).toHaveAttribute(
			"aria-valuenow",
			"25",
		);
		await expect(
			strip.getByRole("link", { name: "Watch Resume fixture" }),
		).toHaveAttribute("href", "/dashboard/watch/41");
	});
});
