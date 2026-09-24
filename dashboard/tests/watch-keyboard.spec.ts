import { expect, test, type Page } from "@playwright/test";
import { fulfillRangeFixture, videoFixture } from "./support/audio";
import {
	mockWatchPage,
	userState,
	videoParts,
	videoRecording,
} from "./support/watch";

// A recording without a playback artifact plays as a sequence of parts. The
// seek keys step through the recording, so a step that crosses a part boundary
// loads the neighbouring part at the matching offset. Vidstack's own seek
// shortcut only moves within the file it has loaded and stops at its edges.

const VIDEO_ID = 86;
const PART_SECONDS = [10, 20];

test.describe("video player keyboard", () => {
	test.use({ viewport: { width: 1536, height: 768 } });

	test("ArrowRight near the end of a part lands in the next part", async ({
		page,
	}) => {
		await openFocusedPlayer(page, 8);

		await page.keyboard.press("ArrowRight");

		await expectPosition(page, { part: 2, partSeconds: 8, seconds: 18 });
	});

	test("ArrowLeft near the start of a part returns to the previous part", async ({
		page,
	}) => {
		await openFocusedPlayer(page, 12);

		await page.keyboard.press("ArrowLeft");

		await expectPosition(page, { part: 1, partSeconds: 2, seconds: 2 });
	});

	// j and l are the same shortcut as the arrows in Vidstack's key map.
	test("steps across parts with l and j like the arrows", async ({ page }) => {
		await openFocusedPlayer(page, 8);

		await page.keyboard.press("l");
		await expectPosition(page, { part: 2, partSeconds: 8, seconds: 18 });

		await page.keyboard.press("j");
		await expectPosition(page, { part: 1, partSeconds: 8, seconds: 8 });
	});

	// Vidstack toggles from its own state, which follows the media's events,
	// so each press waits for the player to report the one before it.
	test("keeps the player's other shortcuts", async ({ page }) => {
		await openFocusedPlayer(page, 8);
		const player = page.locator("[data-media-player]");

		await page.keyboard.press("m");
		await expect(player).toHaveAttribute("data-muted");
		expect(await media(page, "muted")).toBe(true);
		await page.keyboard.press("m");
		await expect(player).not.toHaveAttribute("data-muted");
		expect(await media(page, "muted")).toBe(false);

		await page.keyboard.press("ArrowDown");
		await expect.poll(() => media(page, "volume")).toBeLessThan(1);

		await page.keyboard.press("k");
		await expect(player).toHaveAttribute("data-playing");
		expect(await media(page, "paused")).toBe(false);
		await page.keyboard.press("Space");
		await expect(player).toHaveAttribute("data-paused");
		expect(await media(page, "paused")).toBe(true);

		await page.keyboard.press("f");
		await expect(player).toHaveAttribute("data-fullscreen");
	});
});

// openFocusedPlayer opens the recording paused at a saved position, the way a
// viewer comes back to it, and gives the player keyboard focus.
async function openFocusedPlayer(page: Page, positionSeconds: number) {
	await page.route(`**/api/v1/videos/${VIDEO_ID}/parts/*/stream`, (route) =>
		fulfillRangeFixture(route, videoFixture, "video/mp4"),
	);
	const recording = videoRecording(VIDEO_ID, 30, {
		parts: videoParts(...PART_SECONDS),
		user_state: userState(positionSeconds),
	});
	await mockWatchPage(page, { video: () => recording });
	await page.goto(`/dashboard/watch/${VIDEO_ID}`);
	await expect(page.locator("video")).toBeAttached({ timeout: 30_000 });
	const part = positionSeconds < PART_SECONDS[0] ? 1 : 2;
	const partSeconds =
		part === 1 ? positionSeconds : positionSeconds - PART_SECONDS[0];
	await expect
		.poll(() => media(page, "currentSrc"), { timeout: 15_000 })
		.toContain(`/parts/${part}/stream`);
	await expect
		.poll(() => media(page, "currentTime"), { timeout: 15_000 })
		.toBeGreaterThanOrEqual(partSeconds);
	await page.locator("[data-media-player]").focus();
}

async function expectPosition(
	page: Page,
	{
		part,
		partSeconds,
		seconds,
	}: { part: number; partSeconds: number; seconds: number },
) {
	await expect
		.poll(() => media(page, "currentSrc"))
		.toContain(`/parts/${part}/stream`);
	await expect
		.poll(() => media(page, "currentTime"))
		.toBeCloseTo(partSeconds, 0);
	await expect(
		page.getByRole("slider", { name: "Seek recording", includeHidden: true }),
	).toHaveAttribute("aria-valuenow", String(seconds));
}

function media<
	K extends "currentSrc" | "currentTime" | "muted" | "paused" | "volume",
>(page: Page, key: K) {
	return page
		.locator("video")
		.evaluate((video: HTMLVideoElement, name) => video[name], key);
}
