import { Buffer } from "node:buffer";
import { expect, test, type Page } from "@playwright/test";
import { audioDurationSeconds, fulfillAudioFixture } from "./support/audio";
import { mockTrpc, trpcOk, validSession } from "./support/trpc";

const recordedAt = "2026-06-05T12:00:00Z";
const thumbnailFixture = Buffer.from(
	"iVBORw0KGgoAAAANSUhEUgAAABAAAAAJCAIAAAC0SDtlAAABCElEQVR42g3LIQEFIRBAwY1AAAQRkMiLgCAAETYCARAX4SSSCAjEk0TYCET4f/yICE4IQhQeIQtVUKEJr/AJU1jCEUy4gojHeYIneh5P9lSPeprn9Xye6Vme4zHP9f+QcImQiIknkRM1oYmWeBNfYiZW4iQscdM/FFwhFGLhKeRCLWihFd7CV5iFVTgFK9zyD4pTghKVR8lKVVRpyqt8ylSWchRTrv5Dx3VCJ3aeTu7UjnZa5+18ndlZndOxzu3/MHCDMIiDZ5AHdaCDNngH32AO1uAMbHDHP2zcJmzi5tnkTd3opm3ezbeZm7U5G9vc/Q+GM4IRjcfIRjXUaMZrfMY0lnEMM67xA9Zf8wGO3X2RAAAAAElFTkSuQmCC",
	"base64",
);

test.describe("audio watch player", () => {
	test.use({ viewport: { width: 1536, height: 768 } });

	test("can seek away from the visible end progress handle", async ({
		page,
	}) => {
		await mockWatchAudio(page);
		await page.goto("/dashboard/watch/65");

		const recordingSlider = page.getByRole("slider", {
			name: "Seek recording",
		});
		await expect(recordingSlider).toBeVisible({ timeout: 30_000 });

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expect
			.poll(
				async () =>
					page
						.locator("audio")
						.evaluate((audio: HTMLMediaElement) => audio.ended),
				{ timeout: 30_000 },
			)
			.toBe(true);
		await expect(recordingSlider).toHaveAttribute(
			"aria-valuenow",
			String(audioDurationSeconds),
		);

		const thumb = page.getByTestId("recording-progress-thumb");
		await expect(thumb).toBeVisible();
		const box = await thumb.boundingBox();
		expect(box).not.toBeNull();
		const railBox = await page
			.getByTestId("recording-timeline-rail")
			.boundingBox();
		expect(railBox).not.toBeNull();

		const startX = box!.x + box!.width - 1;
		const startY = box!.y + box!.height / 2;
		const earlyX = railBox!.x + railBox!.width * 0.25;
		await expect
			.poll(async () =>
				page.evaluate(
					({ x, y }) => {
						const element = document.elementFromPoint(x, y);
						return {
							label: element?.getAttribute("aria-label") ?? null,
							role: element?.getAttribute("role") ?? null,
						};
					},
					{ x: startX, y: startY },
				),
			)
			.toEqual({ label: "Seek recording", role: "slider" });

		await page.mouse.move(startX, startY);
		await page.mouse.down();
		await page.mouse.move(earlyX, startY, { steps: 6 });
		await page.mouse.up();

		await expect
			.poll(async () =>
				Number(await recordingSlider.getAttribute("aria-valuenow")),
			)
			.toBeLessThan(audioDurationSeconds);
		await expect(recordingSlider).toHaveAttribute("aria-valuenow", "1");
		await expect
			.poll(async () =>
				page.locator("audio").evaluate((audio: HTMLMediaElement) => ({
					currentTime: audio.currentTime,
					ended: audio.ended,
				})),
			)
			.toMatchObject({ ended: false });
		await expect
			.poll(async () =>
				page
					.locator("audio")
					.evaluate((audio: HTMLMediaElement) => audio.currentTime),
			)
			.toBeLessThan(audioDurationSeconds);

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expectAudioPlaybackToStayPastStart(page);
	});

	test("keeps the stream thumbnail left of the controls and the waveform", async ({
		page,
	}) => {
		await mockWatchAudio(page);
		await page.goto("/dashboard/watch/65");

		const thumbnail = page.getByTestId("audio-thumbnail");
		await expect(thumbnail).toBeVisible({ timeout: 30_000 });
		await expect
			.poll(async () =>
				thumbnail
					.locator("img")
					.evaluate((image: HTMLImageElement) => image.naturalHeight),
			)
			.toBeGreaterThan(0);
		const naturalRatio = await thumbnail
			.locator("img")
			.evaluate(
				(image: HTMLImageElement) => image.naturalWidth / image.naturalHeight,
			);

		const thumbnailBox = await thumbnail.boundingBox();
		expect(thumbnailBox).not.toBeNull();
		const controlsBox = await page.getByTestId("audio-controls").boundingBox();
		expect(controlsBox).not.toBeNull();
		const railBox = await page
			.getByTestId("recording-timeline-rail")
			.boundingBox();
		expect(railBox).not.toBeNull();

		for (const box of [controlsBox, railBox]) {
			expect(thumbnailBox!.x + thumbnailBox!.width).toBeLessThanOrEqual(box!.x);
		}
		expect(thumbnailBox!.width / thumbnailBox!.height).toBeCloseTo(
			naturalRatio,
			1,
		);
		const stackBox = await page.getByTestId("audio-stack").boundingBox();
		expect(stackBox).not.toBeNull();
		expect(thumbnailBox!.y + thumbnailBox!.height / 2).toBeCloseTo(
			stackBox!.y + stackBox!.height / 2,
			0,
		);
		expect(thumbnailBox!.height).toBeLessThanOrEqual(stackBox!.height);
	});

	for (const waveform of [true, false]) {
		test(`shows timeline details below the ${waveform ? "waveform" : "compact scrubber"} above following content`, async ({
			page,
		}) => {
			await mockWatchAudio(page, { timeline: true, waveform });
			await page.goto("/dashboard/watch/65");
			await expect(page.getByTestId("audio-waveform")).toHaveCount(
				waveform ? 1 : 0,
			);
			const rail = page.getByTestId("recording-timeline-rail");
			await expect(rail).toBeVisible();
			await expect(rail).toHaveCSS("height", waveform ? "80px" : "6px");
			for (const button of [
				page.getByRole("button", { name: /A timeline change/ }),
				page.getByRole("button", { name: /^Part 1,/ }),
			]) {
				await button.hover();
				const popover = button.locator(":scope > span").first();
				await expect(popover).toBeVisible();
				const railBox = await rail.boundingBox();
				const popoverBox = await popover.boundingBox();
				const followingBox = await page
					.locator(".rv-watch-player-audio + *")
					.boundingBox();
				expect(popoverBox!.y).toBeGreaterThanOrEqual(
					railBox!.y + railBox!.height,
				);
				expect(popoverBox!.y + popoverBox!.height - 4).toBeGreaterThan(
					followingBox!.y,
				);
				expect(
					await popover.evaluate((element) => {
						const old = element.style.pointerEvents;
						element.style.pointerEvents = "auto";
						const box = element.getBoundingClientRect();
						const top = document.elementFromPoint(
							box.x + box.width / 2,
							box.y + box.height - 4,
						);
						element.style.pointerEvents = old;
						return top !== null && element.contains(top);
					}),
				).toBe(true);
			}
		});
	}

	test("can click an earlier waveform position after playback ended", async ({
		page,
	}) => {
		await mockWatchAudio(page);
		await page.goto("/dashboard/watch/65");

		const recordingSlider = page.getByRole("slider", {
			name: "Seek recording",
		});
		await expect(recordingSlider).toBeVisible({ timeout: 30_000 });

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expect
			.poll(
				async () =>
					page
						.locator("audio")
						.evaluate((audio: HTMLMediaElement) => audio.ended),
				{ timeout: 30_000 },
			)
			.toBe(true);

		const railBox = await page
			.getByTestId("recording-timeline-rail")
			.boundingBox();
		expect(railBox).not.toBeNull();
		await page.mouse.click(
			railBox!.x + railBox!.width * 0.25,
			railBox!.y + railBox!.height / 2,
		);

		await expect(recordingSlider).toHaveAttribute("aria-valuenow", "1");
		await expect
			.poll(async () =>
				page.locator("audio").evaluate((audio: HTMLMediaElement) => ({
					currentTime: audio.currentTime,
					ended: audio.ended,
				})),
			)
			.toMatchObject({ ended: false });
		await expect
			.poll(async () =>
				page
					.locator("audio")
					.evaluate((audio: HTMLMediaElement) => audio.currentTime),
			)
			.toBeLessThan(audioDurationSeconds);

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expectAudioPlaybackToStayPastStart(page);
	});

	test("resumes from an earlier seek after the user scrubbed to the end", async ({
		page,
	}) => {
		await mockWatchAudio(page);
		await page.goto("/dashboard/watch/65");

		const recordingSlider = page.getByRole("slider", {
			name: "Seek recording",
		});
		await expect(recordingSlider).toBeVisible({ timeout: 30_000 });

		const railBox = await page
			.getByTestId("recording-timeline-rail")
			.boundingBox();
		expect(railBox).not.toBeNull();
		await page.mouse.click(
			railBox!.x + railBox!.width - 1,
			railBox!.y + railBox!.height / 2,
		);
		await expect(recordingSlider).toHaveAttribute(
			"aria-valuenow",
			String(audioDurationSeconds),
		);

		await page.mouse.click(
			railBox!.x + railBox!.width * 0.25,
			railBox!.y + railBox!.height / 2,
		);
		await expect(recordingSlider).toHaveAttribute("aria-valuenow", "1");

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expectAudioPlaybackToStayPastStart(page);
		await expect
			.poll(async () =>
				page
					.locator("audio")
					.evaluate((audio: HTMLMediaElement) => audio.currentTime),
			)
			.toBeLessThan(audioDurationSeconds);
	});

	test("does not replay from zero when play is pressed immediately after seeking away from the end", async ({
		page,
	}) => {
		await mockWatchAudio(page);
		await page.goto("/dashboard/watch/65");

		const recordingSlider = page.getByRole("slider", {
			name: "Seek recording",
		});
		await expect(recordingSlider).toBeVisible({ timeout: 30_000 });

		const railBox = await page
			.getByTestId("recording-timeline-rail")
			.boundingBox();
		expect(railBox).not.toBeNull();

		await page.mouse.click(
			railBox!.x + railBox!.width - 1,
			railBox!.y + railBox!.height / 2,
		);
		await expect(recordingSlider).toHaveAttribute(
			"aria-valuenow",
			String(audioDurationSeconds),
		);

		await page.mouse.click(
			railBox!.x + railBox!.width * 0.25,
			railBox!.y + railBox!.height / 2,
		);
		await page.getByRole("button", { name: "Play", exact: true }).click();

		await expectAudioPlaybackToStayPastStart(page);
	});
});

async function expectAudioPlaybackToStayPastStart(page: Page) {
	const samples = await page.locator("audio").evaluate(async (element) => {
		const audio = element as HTMLAudioElement;
		const values: number[] = [];
		const startedAt = performance.now();
		while (performance.now() - startedAt < 750) {
			values.push(audio.currentTime);
			await new Promise((resolve) => setTimeout(resolve, 50));
		}
		return values;
	});
	expect(samples.length).toBeGreaterThan(0);
	expect(Math.min(...samples)).toBeGreaterThan(0.5);
	expect(samples.at(-1) ?? 0).toBeGreaterThan(1);
}

async function mockWatchAudio(
	page: Parameters<typeof mockTrpc>[0],
	options: { timeline?: boolean; waveform?: boolean } = {},
) {
	const video = audioVideo();
	if (options.timeline) {
		video.duration_seconds *= 2;
		video.parts.push({
			...video.parts[0],
			part_index: 2,
			filename: "steam-nukes-indies-part2.m4a",
		});
	}
	await page.route("**/api/v1/videos/65/waveform", async (route) => {
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				duration_seconds: video.duration_seconds,
				peaks:
					options.waveform === false
						? []
						: Array.from({ length: 96 }, (_, index) =>
								Number(
									(0.2 + Math.abs(Math.sin(index * 0.3)) * 0.75).toFixed(3),
								),
							),
			}),
		});
	});

	await page.route("**/api/v1/videos/65/parts/1/stream", async (route) => {
		await fulfillAudioFixture(route);
	});

	await page.route("**/api/v1/thumbnails/**", async (route) => {
		await route.fulfill({
			status: 200,
			contentType: "image/png",
			body: thumbnailFixture,
		});
	});

	await mockTrpc(page, (procs) => {
		const session = validSession(procs, "");
		if (session) return session;

		if (procs.includes("video.getById")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) => (proc === "video.getById" ? video : null)),
				),
			};
		}
		if (procs.includes("video.timeline")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.timeline"
							? options.timeline
								? [
										{
											occurred_at: recordedAt,
											media_offset_seconds: audioDurationSeconds,
											title: { id: 1, name: "A timeline change" },
										},
									]
								: []
							: null,
					),
				),
			};
		}
		if (procs.includes("video.categories")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.categories"
							? [
									{
										id: "software",
										name: "Software and Game Development",
										started_at: recordedAt,
										duration_seconds: audioDurationSeconds,
									},
								]
							: null,
					),
				),
			};
		}
		if (procs.includes("video.titles")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.titles"
							? [
									{
										id: 1,
										name: "Steam nukes indies. GL HF.",
										started_at: recordedAt,
										duration_seconds: audioDurationSeconds,
									},
								]
							: null,
					),
				),
			};
		}
		if (procs.includes("channel.getById")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "channel.getById"
							? {
									broadcaster_id: "chan1",
									broadcaster_login: "thornityco",
									broadcaster_name: "ThornityCo",
									profile_image_url: "",
									view_count: 0,
									created_at: recordedAt,
									updated_at: recordedAt,
								}
							: null,
					),
				),
			};
		}
		if (procs.includes("video.statisticsByBroadcaster")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.statisticsByBroadcaster"
							? {
									total: 1,
									total_size: 99_000_000,
									total_duration_seconds: audioDurationSeconds,
								}
							: null,
					),
				),
			};
		}
		if (procs.includes("stream.latestLive")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) => (proc === "stream.latestLive" ? [] : null)),
				),
			};
		}

		return null;
	});
}

function audioVideo() {
	return {
		id: 65,
		job_id: "job-65",
		filename: "steam-nukes-indies",
		display_name: "ThornityCo",
		title: "Steam nukes indies. GL HF.",
		status: "DONE",
		completion_kind: "complete",
		truncated: false,
		quality: "audio_only",
		codec: "aac",
		is_audio_only: true,
		thumbnail: "thumbnails/steam-nukes-indies.jpg",
		broadcaster_id: "chan1",
		broadcaster_login: "thornityco",
		broadcaster_name: "ThornityCo",
		profile_image_url: "",
		primary_category_id: "software",
		primary_category_name: "Software and Game Development",
		viewer_count: 0,
		language: "en",
		duration_seconds: audioDurationSeconds,
		size_bytes: 99_000_000,
		start_download_at: recordedAt,
		downloaded_at: recordedAt,
		parts: [
			{
				part_index: 1,
				filename: "steam-nukes-indies-part1.m4a",
				quality: "audio_only",
				codec: "aac",
				segment_format: "fmp4",
				duration_seconds: audioDurationSeconds,
				size_bytes: 99_000_000,
				start_media_seq: 0,
				end_media_seq: 12,
			},
		],
		playback_artifact: {
			status: "unavailable",
			updated_at: recordedAt,
		},
	};
}
