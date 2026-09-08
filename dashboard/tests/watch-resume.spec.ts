import { expect, test, type Page } from "@playwright/test";
import { fulfillRangeFixture, silentWavFixture } from "./support/audio";
import { audioOnlyVideo, mockWatchPage, userState } from "./support/watch";

// Resume on the watch page: the player saves where the listener is and opens
// there again after a reload, with the same rules a viewer expects from any
// media library (not from the first seconds, not from the very end, and a
// deep link wins).

const VIDEO_ID = 77;
const DURATION_SECONDS = 30;
const fixture = silentWavFixture(DURATION_SECONDS);

type ProgressWrite = {
	video_id: number;
	position_seconds: number;
	completed: boolean;
};

type ResumeState = {
	position: number;
	writes: ProgressWrite[];
	// While set, progress writes answer 500 and are not recorded.
	failing: boolean;
	failed: number;
};

test.describe("watch page resume", () => {
	test.use({ viewport: { width: 1536, height: 768 } });

	test("resumes where the listener paused after a reload", async ({ page }) => {
		const state = await mockWatchResume(page);
		await page.goto(`/dashboard/watch/${VIDEO_ID}`);
		await expect(seekSlider(page)).toBeVisible({ timeout: 30_000 });

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expect
			.poll(() => currentTime(page), { timeout: 15_000 })
			.toBeGreaterThan(6);
		await page.getByRole("button", { name: "Pause", exact: true }).click();
		await expect
			.poll(() => state.writes.at(-1)?.position_seconds ?? 0)
			.toBeGreaterThan(6);
		const saved = state.position;
		expect(state.writes.every((write) => !write.completed)).toBe(true);

		await page.reload();
		await expect(seekSlider(page)).toBeVisible({ timeout: 30_000 });
		await expect
			.poll(() => currentTime(page))
			.toBeGreaterThanOrEqual(saved - 0.25);
		expect(await isPaused(page)).toBe(true);
		await expect(seekSlider(page)).toHaveAttribute(
			"aria-valuenow",
			String(Math.round(saved)),
		);
		await expect(page.getByTestId("resume-notice")).toContainText(
			"Resumed from",
		);

		// Playback carries on from the saved place, not from the start.
		await page.getByRole("button", { name: "Play", exact: true }).click();
		const samples = await sampleCurrentTime(page, 750);
		expect(Math.min(...samples)).toBeGreaterThan(saved - 0.25);
		expect(samples.at(-1) ?? 0).toBeGreaterThan(saved + 0.5);
	});

	test("keeps playing in place while a progress write is in flight", async ({
		page,
	}) => {
		const state = await mockWatchResume(page, { responseDelayMs: 400 });
		await page.goto(`/dashboard/watch/${VIDEO_ID}`);
		await expect(seekSlider(page)).toBeVisible({ timeout: 30_000 });

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expect.poll(() => state.writes.length).toBeGreaterThan(0);

		// The saved position landing in the video cache must not seek the
		// player back to it: time only moves forward.
		const samples = await sampleCurrentTime(page, 1500);
		for (let index = 1; index < samples.length; index += 1) {
			expect(samples[index]).toBeGreaterThanOrEqual(samples[index - 1] ?? 0);
		}
		expect(samples.at(-1) ?? 0).toBeGreaterThan(2);
	});

	for (const [name, position] of [
		["from the first seconds", 3],
		["from the very end", DURATION_SECONDS - 1],
	] as const) {
		test(`starts over instead of resuming ${name}`, async ({ page }) => {
			await mockWatchResume(page, { position });
			await page.goto(`/dashboard/watch/${VIDEO_ID}`);
			await expect(seekSlider(page)).toBeVisible({ timeout: 30_000 });

			await expect.poll(() => readyState(page)).toBeGreaterThan(0);
			expect(await currentTime(page)).toBe(0);
			await expect(seekSlider(page)).toHaveAttribute("aria-valuenow", "0");
			await expect(page.getByTestId("resume-notice")).toHaveCount(0);
		});
	}

	test("a deep link wins over the saved position", async ({ page }) => {
		await mockWatchResume(page, { position: 12 });
		await page.goto(`/dashboard/watch/${VIDEO_ID}?t=3`);
		await expect(seekSlider(page)).toBeVisible({ timeout: 30_000 });

		await expect.poll(() => currentTime(page)).toBeGreaterThanOrEqual(3);
		expect(await currentTime(page)).toBeLessThan(3.5);
		await expect(seekSlider(page)).toHaveAttribute("aria-valuenow", "3");
		await expect(page.getByTestId("resume-notice")).toHaveCount(0);
	});

	test("offers to start over from the resume notice", async ({ page }) => {
		const state = await mockWatchResume(page, { position: 12 });
		await page.goto(`/dashboard/watch/${VIDEO_ID}`);
		await expect(seekSlider(page)).toBeVisible({ timeout: 30_000 });
		await expect.poll(() => currentTime(page)).toBeGreaterThanOrEqual(12);

		const notice = page.getByTestId("resume-notice");
		await expect(notice).toContainText("Resumed from 0:12");
		await notice.getByRole("button", { name: "Start over" }).click();
		await expect(notice).toHaveCount(0);
		await expect.poll(() => currentTime(page)).toBeLessThan(0.5);
		await expect(seekSlider(page)).toHaveAttribute("aria-valuenow", "0");

		// Leaving now saves the start: the deliberate rewind sticks.
		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expect.poll(() => currentTime(page)).toBeGreaterThan(1);
		await page.getByRole("button", { name: "Pause", exact: true }).click();
		await expect
			.poll(() => state.writes.at(-1)?.position_seconds ?? 99)
			.toBeLessThan(3);
	});

	test("saves the position when leaving for another page", async ({
		page,
	}) => {
		const state = await mockWatchResume(page);
		await page.goto(`/dashboard/watch/${VIDEO_ID}`);
		await expect(seekSlider(page)).toBeVisible({ timeout: 30_000 });

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expect
			.poll(() => currentTime(page), { timeout: 15_000 })
			.toBeGreaterThan(2.5);
		const leftAt = await currentTime(page);
		// The sidebar folds the Library group away from the watch page.
		const library = page.getByRole("button", { name: "Library" });
		if ((await library.getAttribute("aria-expanded")) === "false") {
			await library.click();
		}
		await page.getByRole("link", { name: "Videos", exact: true }).click();
		await expect(page).toHaveURL(/\/dashboard\/videos/);

		await expect
			.poll(() => state.writes.at(-1)?.position_seconds ?? 0)
			.toBeGreaterThanOrEqual(leftAt - 0.25);
	});

	test("saves the position when the page unloads", async ({ page }) => {
		const state = await mockWatchResume(page);
		await page.goto(`/dashboard/watch/${VIDEO_ID}`);
		await expect(seekSlider(page)).toBeVisible({ timeout: 30_000 });

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expect
			.poll(() => currentTime(page), { timeout: 15_000 })
			.toBeGreaterThan(2.5);
		const leftAt = await currentTime(page);
		// A full navigation tears the document down. Playwright's network layer
		// no longer reports requests the dying document issues, so the write is
		// caught inside the page: fetch is wrapped to record what pagehide sends,
		// and the record is read back on the next same-origin page.
		await page.evaluate(() => {
			window.addEventListener("pagehide", () => {
				window.localStorage.setItem("e2e:pagehide", "fired");
			});
			const original = window.fetch;
			window.fetch = (input, init) => {
				const url =
					typeof input === "string"
						? input
						: input instanceof URL
							? input.href
							: input.url;
				if (url.includes("/trpc/video.updateWatchProgress")) {
					window.localStorage.setItem(
						"e2e:unload-write",
						JSON.stringify({
							body: JSON.parse(String(init?.body ?? "null")),
							keepalive: init?.keepalive === true,
						}),
					);
				}
				return original(input, init);
			};
		});
		await page.goto("/login");
		const record = await page.evaluate(() => {
			const raw = window.localStorage.getItem("e2e:unload-write");
			const pagehide = window.localStorage.getItem("e2e:pagehide");
			window.localStorage.removeItem("e2e:unload-write");
			window.localStorage.removeItem("e2e:pagehide");
			return {
				pagehide,
				write: raw
					? (JSON.parse(raw) as { body: ProgressWrite; keepalive: boolean })
					: null,
			};
		});
		expect(record.pagehide).toBe("fired");
		expect(record.write?.keepalive).toBe(true);
		expect(record.write?.body.video_id).toBe(VIDEO_ID);
		expect(record.write?.body.position_seconds ?? 0).toBeGreaterThanOrEqual(
			leftAt - 0.25,
		);
		expect(state.writes.length).toBeGreaterThan(0);
	});

	test("replays a write the server missed once it is back", async ({
		page,
	}) => {
		const state = await mockWatchResume(page);
		await page.goto(`/dashboard/watch/${VIDEO_ID}`);
		await expect(seekSlider(page)).toBeVisible({ timeout: 30_000 });

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expect
			.poll(() => currentTime(page), { timeout: 15_000 })
			.toBeGreaterThan(6);
		state.failing = true;
		await page.getByRole("button", { name: "Pause", exact: true }).click();
		await expect.poll(() => state.failed).toBeGreaterThan(0);
		const pausedAt = await currentTime(page);
		expect(state.position).toBeLessThan(pausedAt);

		// The server is back: the reload opens at the unconfirmed position and
		// sends it again so the server catches up.
		state.failing = false;
		await page.reload();
		await expect(seekSlider(page)).toBeVisible({ timeout: 30_000 });
		await expect
			.poll(() => currentTime(page))
			.toBeGreaterThanOrEqual(pausedAt - 0.25);
		await expect
			.poll(() => state.position)
			.toBeGreaterThanOrEqual(pausedAt - 0.25);
	});
});

function seekSlider(page: Page) {
	return page.getByRole("slider", { name: "Seek recording" });
}

function currentTime(page: Page) {
	return page
		.locator("audio")
		.evaluate((audio: HTMLMediaElement) => audio.currentTime);
}

function readyState(page: Page) {
	return page
		.locator("audio")
		.evaluate((audio: HTMLMediaElement) => audio.readyState);
}

function isPaused(page: Page) {
	return page
		.locator("audio")
		.evaluate((audio: HTMLMediaElement) => audio.paused);
}

async function sampleCurrentTime(page: Page, durationMs: number) {
	const samples = await page
		.locator("audio")
		.evaluate(async (element, windowMs) => {
			const audio = element as HTMLAudioElement;
			const values: number[] = [];
			const startedAt = performance.now();
			while (performance.now() - startedAt < windowMs) {
				values.push(audio.currentTime);
				await new Promise((resolve) => setTimeout(resolve, 50));
			}
			return values;
		}, durationMs);
	expect(samples.length).toBeGreaterThan(0);
	return samples;
}

// mockWatchResume serves the recording and keeps the saved position in
// `state`: every video.updateWatchProgress write moves it, and video.getById
// hands it back, so a reload sees what the player saved.
async function mockWatchResume(
	page: Page,
	{
		position = 0,
		responseDelayMs = 0,
	}: { position?: number; responseDelayMs?: number } = {},
): Promise<ResumeState> {
	const state: ResumeState = { position, writes: [], failing: false, failed: 0 };

	await page.route(`**/api/v1/videos/${VIDEO_ID}/waveform`, async (route) => {
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				duration_seconds: DURATION_SECONDS,
				peaks: Array.from({ length: 96 }, () => 0.5),
			}),
		});
	});
	await page.route(
		`**/api/v1/videos/${VIDEO_ID}/parts/1/stream`,
		async (route) => {
			await fulfillRangeFixture(route, fixture, "audio/wav");
		},
	);
	await mockWatchPage(page, {
		video: () =>
			audioOnlyVideo(VIDEO_ID, DURATION_SECONDS, {
				user_state: userState(state.position),
			}),
	});
	// Registered after the batch mock so it wins: progress writes go out on
	// their own keepalive request, batched or not.
	await page.route("**/trpc/video.updateWatchProgress*", async (route) => {
		try {
			if (state.failing) {
				state.failed += 1;
				await route.fulfill({ status: 500, body: "server restarting" });
				return;
			}
			const body = route.request().postDataJSON() as
				| ProgressWrite
				| Record<string, ProgressWrite>;
			const batched = !("video_id" in body);
			const input = batched
				? (body as Record<string, ProgressWrite>)["0"]
				: (body as ProgressWrite);
			if (input) {
				state.writes.push(input);
				state.position = input.position_seconds;
			}
			if (responseDelayMs > 0) {
				await new Promise((resolve) => setTimeout(resolve, responseDelayMs));
			}
			const envelope = { result: { data: userState(state.position) } };
			await route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify(batched ? [envelope] : envelope),
			});
		} catch {
			// The page is unloading (reload, close); the write was recorded.
		}
	});
	return state;
}
