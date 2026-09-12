import { expect, test } from "@playwright/test";
import { fulfillRangeFixture, videoFixture } from "./support/audio";
import { mockWatchPage, videoRecording } from "./support/watch";

test("desktop fullscreen never requests an unsupported screen orientation lock", async ({
	page,
}) => {
	const lockRequests: unknown[] = [];
	await page.exposeFunction("recordOrientationLock", (orientation: unknown) =>
		lockRequests.push(orientation),
	);
	await page.addInitScript(() => {
		const orientation = screen.orientation as ScreenOrientation & {
			lock: (value: string) => Promise<void>;
		};
		const lock = orientation.lock.bind(orientation);
		orientation.lock = (value: string) => {
			void (
				window as unknown as {
					recordOrientationLock: (value: string) => Promise<void>;
				}
			).recordOrientationLock(value);
			return lock(value);
		};
	});
	await page.route("**/api/v1/videos/78/parts/1/stream", (route) =>
		fulfillRangeFixture(route, videoFixture, "video/mp4"),
	);
	await mockWatchPage(page, { video: () => videoRecording(78, 30) });
	await page.goto("/dashboard/watch/78");
	await expect(page.locator("video")).toBeAttached();
	await page.getByRole("button", { name: "Play", exact: true }).first().click();
	await expect
		.poll(() =>
			page
				.locator("video")
				.evaluate((video: HTMLVideoElement) => video.currentTime),
		)
		.toBeGreaterThan(0);
	await page
		.locator("video")
		.evaluate((video: HTMLVideoElement) => video.pause());
	// Exercise the real Vidstack fullscreen controller and browser events,
	// including a second entry after unlock. Chromium exposes lock on desktop
	// but rejects it, which is why API presence alone cannot authorize a lock.
	for (let entry = 0; entry < 2; entry++) {
		await page
			.getByRole("button", { name: "Fullscreen", exact: true })
			.click();
		await expect
			.poll(() => page.evaluate(() => document.fullscreenElement !== null))
			.toBe(true);
		await page
			.getByRole("button", { name: "Fullscreen", exact: true })
			.click();
		await expect
			.poll(() => page.evaluate(() => document.fullscreenElement === null))
			.toBe(true);
	}
	expect(lockRequests).toEqual([]);
});
