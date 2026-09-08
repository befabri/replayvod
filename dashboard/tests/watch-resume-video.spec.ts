import { expect, test, type Page } from "@playwright/test";
import { fulfillRangeFixture, videoFixture } from "./support/audio";
import { mockWatchPage, userState, videoRecording } from "./support/watch";

// The video player (Vidstack) shares the resume path with the audio player:
// a saved position seeds the seek that runs once the source can play, the
// pause flush saves the exact place, and a reload opens there.

const VIDEO_ID = 78;
const DURATION_SECONDS = 30;

type ProgressWrite = {
	video_id: number;
	position_seconds: number;
	completed: boolean;
};

test.describe("video watch page resume", () => {
	test.use({ viewport: { width: 1536, height: 768 } });

	test("resumes where the viewer paused after a reload", async ({ page }) => {
		const state = await mockVideoResume(page);
		await page.goto(`/dashboard/watch/${VIDEO_ID}`);
		await expect(page.locator("video")).toBeAttached({ timeout: 30_000 });

		await page.getByRole("button", { name: "Play", exact: true }).first().click();
		await expect
			.poll(() => currentTime(page), { timeout: 15_000 })
			.toBeGreaterThan(6);
		// Vidstack hides its controls during playback; pausing the element fires
		// the same pause event the control bar would.
		await page.locator("video").evaluate((video: HTMLMediaElement) => {
			video.pause();
		});
		await expect
			.poll(() => state.writes.at(-1)?.position_seconds ?? 0)
			.toBeGreaterThan(6);
		const saved = state.position;

		await page.reload();
		await expect(page.locator("video")).toBeAttached({ timeout: 30_000 });
		await expect
			.poll(() => currentTime(page), { timeout: 15_000 })
			.toBeGreaterThanOrEqual(saved - 0.25);
		expect(await isPaused(page)).toBe(true);
		await expect(page.getByTestId("resume-notice")).toContainText(
			"Resumed from",
		);
		await expect(
			page.getByRole("slider", { name: "Seek recording" }),
		).toHaveAttribute("aria-valuenow", String(Math.round(saved)));
	});

	test("starts over from the resume notice", async ({ page }) => {
		await mockVideoResume(page, { position: 12 });
		await page.goto(`/dashboard/watch/${VIDEO_ID}`);
		await expect(page.locator("video")).toBeAttached({ timeout: 30_000 });
		await expect
			.poll(() => currentTime(page), { timeout: 15_000 })
			.toBeGreaterThanOrEqual(12);

		await page
			.getByTestId("resume-notice")
			.getByRole("button", { name: "Start over" })
			.click();
		await expect.poll(() => currentTime(page)).toBeLessThan(0.5);
		await expect(page.getByTestId("resume-notice")).toHaveCount(0);
	});
});

function currentTime(page: Page) {
	return page
		.locator("video")
		.evaluate((video: HTMLMediaElement) => video.currentTime);
}

function isPaused(page: Page) {
	return page.locator("video").evaluate((video: HTMLMediaElement) => video.paused);
}

async function mockVideoResume(
	page: Page,
	{ position = 0 }: { position?: number } = {},
) {
	const state = { position, writes: [] as ProgressWrite[] };
	await page.route(
		`**/api/v1/videos/${VIDEO_ID}/parts/1/stream`,
		async (route) => {
			await fulfillRangeFixture(route, videoFixture, "video/mp4");
		},
	);
	await mockWatchPage(page, {
		video: () =>
			videoRecording(VIDEO_ID, DURATION_SECONDS, {
				user_state: userState(state.position),
			}),
	});
	await page.route("**/trpc/video.updateWatchProgress*", async (route) => {
		try {
			const input = route.request().postDataJSON() as ProgressWrite;
			state.writes.push(input);
			state.position = input.position_seconds;
			await route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({ result: { data: userState(state.position) } }),
			});
		} catch {
			// The page is unloading; the write was recorded.
		}
	});
	return state;
}
