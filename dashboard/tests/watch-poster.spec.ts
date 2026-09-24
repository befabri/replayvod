import { expect, test, type Page } from "@playwright/test";
import {
	fulfillAudioFixture,
	fulfillRangeFixture,
	videoFixture,
} from "./support/audio";
import { procsOf, SESSION, trpcOk } from "./support/trpc";
import {
	audioOnlyVideo,
	mockWatchPage,
	userState,
	videoRecording,
} from "./support/watch";

// Opening a recording from the library paints the thumbnail the card already
// showed while video.getById is still in flight, and the player takes the same
// picture over when it mounts. The spec samples every animation frame of that
// handoff: a frame where no fully opaque, loaded copy of the thumbnail covers
// the player area, or where it sits somewhere else, is the flash it guards.

const VIDEO_ID = 81;
const POSTER_FILE = "poster-fixture.svg";
const POSTER_SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="1280" height="720" viewBox="0 0 16 9"><rect width="16" height="9" fill="#2f6f5e"/><circle cx="8" cy="4.5" r="2.5" fill="#f2c14e"/></svg>`;

type PosterFrame = {
	shown: boolean;
	inPlayer: boolean;
	rect: number[] | null;
};

test.describe("watch page poster", () => {
	test.use({ viewport: { width: 1536, height: 768 } });

	test("keeps the library thumbnail on screen until the player takes over", async ({
		page,
	}) => {
		const recording = videoRecording(VIDEO_ID, 30, {
			thumbnail: `thumbnails/${POSTER_FILE}`,
		});
		// Library rows carry no parts or playback artifact; only the detail does.
		const { parts: _parts, playback_artifact: _artifact, ...libraryRow } =
			recording;
		let releaseDetail!: () => void;
		const detailHeld = new Promise<void>((resolve) => {
			releaseDetail = resolve;
		});

		await page.route(`**/api/v1/thumbnails/${POSTER_FILE}`, (route) =>
			route.fulfill({ contentType: "image/svg+xml", body: POSTER_SVG }),
		);
		await page.route(
			`**/api/v1/videos/${VIDEO_ID}/parts/1/stream`,
			async (route) => {
				await fulfillRangeFixture(route, videoFixture, "video/mp4");
			},
		);
		await mockWatchPage(page, {
			video: () => recording,
			resolve: (procs) =>
				procs.includes("video.listPage")
					? {
							status: 200,
							body: trpcOk(
								procs.map((proc) =>
									proc === "auth.session"
										? SESSION
										: proc === "video.listPage"
											? { items: [libraryRow] }
											: null,
								),
							),
						}
					: null,
		});
		// Registered last, so it runs first and holds any batch asking for the
		// detail, whether the hover preload or the page itself sent it.
		await page.route("**/trpc/**", async (route) => {
			if (procsOf(route.request().url()).includes("video.getById")) {
				await detailHeld;
			}
			await route.fallback();
		});

		await page.goto("/dashboard/videos");
		const card = page.getByRole("link", { name: "Watch Resume fixture" });
		await expect(card).toBeVisible();
		await expect
			.poll(() =>
				card
					.locator("img")
					.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth),
			)
			.toBeGreaterThan(0);

		await startPosterSampler(page);
		await card.click();
		await expect
			.poll(async () =>
				(await posterFrames(page)).some((f) => f.shown && !f.inPlayer),
			)
			.toBe(true);
		await page.waitForTimeout(250);

		releaseDetail();
		await expect(page.locator("video")).toBeAttached({ timeout: 30_000 });
		await expect
			.poll(async () => (await posterFrames(page)).at(-1)?.inPlayer)
			.toBe(true);
		// Long enough for any fade or deferred poster load in the player to show.
		await page.waitForTimeout(600);

		const frames = await stopPosterSampler(page);
		const first = frames.findIndex((f) => f.shown);
		expect(first).toBeGreaterThanOrEqual(0);
		const handoff = frames.slice(first);
		const gaps = handoff.flatMap((f, i) => (f.shown ? [] : [i]));
		expect(gaps, "frames without the poster painted").toEqual([]);
		expect(handoff.some((f) => !f.inPlayer)).toBe(true);
		expect(handoff.at(-1)?.inPlayer).toBe(true);
		const [x, y, width, height] = handoff[0].rect ?? [];
		for (const frame of handoff) {
			const [fx, fy, fw, fh] = frame.rect ?? [];
			expect(Math.abs(fx - x), "poster moved").toBeLessThanOrEqual(1);
			expect(Math.abs(fy - y), "poster moved").toBeLessThanOrEqual(1);
			expect(Math.abs(fw - width), "poster resized").toBeLessThanOrEqual(1);
			expect(Math.abs(fh - height), "poster resized").toBeLessThanOrEqual(1);
		}
	});
});

test.describe("watch page poster after the handoff", () => {
	test.use({ viewport: { width: 1536, height: 768 } });

	async function openVideo(page: Page, overrides: Record<string, unknown>) {
		await page.route(`**/api/v1/thumbnails/${POSTER_FILE}`, (route) =>
			route.fulfill({ contentType: "image/svg+xml", body: POSTER_SVG }),
		);
		await page.route(
			`**/api/v1/videos/${VIDEO_ID}/parts/1/stream`,
			async (route) => {
				await fulfillRangeFixture(route, videoFixture, "video/mp4");
			},
		);
		const recording = videoRecording(VIDEO_ID, 30, {
			thumbnail: `thumbnails/${POSTER_FILE}`,
			...overrides,
		});
		await mockWatchPage(page, { video: () => recording });
		await page.goto(`/dashboard/watch/${VIDEO_ID}`);
		await expect(page.locator("video")).toBeAttached({ timeout: 30_000 });
	}

	// Resuming seeks before anyone presses play, and Vidstack never reports
	// `started` for a seek. The resume seek is the page's own, so the poster
	// stays over it; a seek the viewer makes shows them the frame they chose.
	test("keeps the poster over the resume point and drops it for a viewer's seek", async ({
		page,
	}) => {
		await openVideo(page, { user_state: userState(12) });
		await expect
			.poll(() => mediaTime(page), { timeout: 15_000 })
			.toBeGreaterThanOrEqual(12);
		const poster = page.getByTestId("player-poster");
		await page.waitForTimeout(400);
		await expect(poster).not.toHaveAttribute("data-dismissed");

		await page
			.getByTestId("resume-notice")
			.getByRole("button", { name: "Start over" })
			.click();

		await expect.poll(() => mediaTime(page)).toBeLessThan(0.5);
		await expect(poster).toHaveAttribute("data-dismissed");
		expect(await isPaused(page)).toBe(true);
	});

	// The poster is a frame of the same recording. Cropping it while the video
	// letterboxes would jump in scale the moment it fades out.
	test("frames the poster the way the player frames the video", async ({
		page,
	}) => {
		await openVideo(page, {});
		const fits = await page.evaluate(() => {
			const poster = document.querySelector('[data-testid="player-poster"]');
			const video = document.querySelector("video");
			return [poster, video].map((element) =>
				element ? getComputedStyle(element).objectFit : null,
			);
		});
		expect(fits[0]).not.toBeNull();
		expect(fits[0]).toBe(fits[1]);
	});
});

test.describe("watch page without a thumbnail", () => {
	test.use({ viewport: { width: 1536, height: 768 } });

	// The server records the first snapshot it wrote as the thumbnail, so an
	// empty field means there is no image. Guessing a snapshot name would ask
	// for a 404, and the audio card would open a box for it and then close it.
	test("asks for no image and keeps no room for one", async ({ page }) => {
		const AUDIO_ID = 83;
		const recording = audioOnlyVideo(AUDIO_ID, 4, { thumbnail: "" });
		const thumbnailRequests: string[] = [];
		await page.route("**/api/v1/thumbnails/**", (route) => {
			thumbnailRequests.push(route.request().url());
			return route.fulfill({ status: 404, body: "" });
		});
		await page.route(
			`**/api/v1/videos/${AUDIO_ID}/parts/1/stream`,
			(route) => fulfillAudioFixture(route),
		);
		await mockWatchPage(page, { video: () => recording });

		await page.goto(`/dashboard/watch/${AUDIO_ID}`);
		await expect(
			page.getByRole("region", { name: /^Audio Player/ }),
		).toBeVisible({ timeout: 30_000 });
		await page.waitForTimeout(300);

		await expect(page.getByTestId("audio-thumbnail")).toHaveCount(0);
		expect(thumbnailRequests).toEqual([]);
	});
});

function mediaTime(page: Page) {
	return page
		.locator("video")
		.evaluate((video: HTMLMediaElement) => video.currentTime);
}

function isPaused(page: Page) {
	return page
		.locator("video")
		.evaluate((video: HTMLMediaElement) => video.paused);
}

// startPosterSampler records, once per animation frame, whether a loaded and
// fully opaque copy of the thumbnail sits in the page outside the library
// card's link, and where. It survives the client-side navigation to the
// watch page because the document never reloads.
function startPosterSampler(page: Page) {
	return page.evaluate((file) => {
		const frames: PosterFrame[] = [];
		const state = { frames, stopped: false };
		Object.assign(window, { __posterSampler: state });
		const opacityOf = (element: Element) => {
			let opacity = 1;
			for (
				let node: Element | null = element;
				node;
				node = node.parentElement
			) {
				opacity *= Number(getComputedStyle(node).opacity);
			}
			return opacity;
		};
		const sample = () => {
			const shown = [...document.querySelectorAll("main img")].find(
				(img): img is HTMLImageElement =>
					img instanceof HTMLImageElement &&
					!img.closest("a") &&
					img.currentSrc.endsWith(file) &&
					img.complete &&
					img.naturalWidth > 0 &&
					img.getBoundingClientRect().width > 0 &&
					opacityOf(img) === 1,
			);
			const rect = shown?.getBoundingClientRect();
			frames.push({
				shown: !!shown,
				inPlayer: !!shown?.closest("[data-media-player]"),
				rect: rect
					? [rect.x, rect.y, rect.width, rect.height].map(Math.round)
					: null,
			});
			if (!state.stopped) requestAnimationFrame(sample);
		};
		requestAnimationFrame(sample);
	}, POSTER_FILE);
}

function posterFrames(page: Page) {
	return page.evaluate(
		() =>
			(window as unknown as { __posterSampler: { frames: PosterFrame[] } })
				.__posterSampler.frames,
	);
}

function stopPosterSampler(page: Page) {
	return page.evaluate(() => {
		const state = (
			window as unknown as {
				__posterSampler: { frames: PosterFrame[]; stopped: boolean };
			}
		).__posterSampler;
		state.stopped = true;
		return state.frames;
	});
}
